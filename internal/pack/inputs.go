package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func prepareInputs(repo repository.Repository, request *Request, commands ...policy.Command) error {
	paths, err := repo.AllFiles()
	if err != nil {
		return err
	}
	command := policy.Command{Name: request.Provider, Adapter: &policy.PackAdapter{Capability: request.Capability, Languages: []policy.LanguageRule{{Name: "source", Paths: []string{"**/*"}}}}}
	if len(commands) > 0 {
		command = commands[0]
	}
	return prepareInputPaths(repo, request, command, paths)
}

func prepareInputPaths(repo repository.Repository, request *Request, command policy.Command, paths []string) error {
	paths = sortedUnique(append(slices.Clone(paths), request.Files...))
	request.Inventory = governedInventory(repo, command, paths, request.Profile)
	if len(request.Scopes) == 0 {
		prepareFileScopes(repo, request)
	}
	if err := validateAnalysisScopes(*request); err != nil {
		return err
	}
	policyValue, err := policyInput(repo, *request)
	if err != nil {
		return err
	}
	request.Policy = policyValue
	paths = analysisContextPaths(*request)
	assetLinks := validatedAssetLinkInputs(repo, paths)
	for path := range assetLinks {
		paths = append(paths, path)
	}
	paths = sortedUnique(paths)
	if len(paths) > 100000 {
		return errors.New("provider context exceeds 100000 files")
	}
	request.Context = make([]InputFile, 0, len(paths))
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range paths {
		data, found := assetLinks[path]
		if !found {
			data, err = readContextInput(repo, root, path)
			if err != nil {
				return err
			}
		}
		request.Context = append(request.Context, InputFile{Path: path, SHA256: inputDigest(data)})
	}
	return nil
}

func prepareFileScopes(repo repository.Repository, request *Request) {
	initializeRequestScope(request)
	entries := inventoryByPath(request.Inventory)
	for index, file := range request.Files {
		entry, found := entries[file]
		if !found || !entry.Source {
			continue
		}
		root := pathDirectory(file)
		entryFiles := []string{}
		if coreEntryPoint(repo, root, file) {
			entryFiles = append(entryFiles, file)
		}
		request.Scopes = append(request.Scopes, AnalysisScope{Handle: fmt.Sprintf("scope-%d", index+1), Language: entry.Language, Root: root, Members: []string{file}, EntryFiles: entryFiles, Context: []string{}, Data: json.RawMessage(`{}`)})
	}
	request.WriteFiles = slices.DeleteFunc(request.WriteFiles, func(file string) bool {
		entry := entries[file]
		return entry.Generated || entry.Data
	})
}

func policyInput(repo repository.Repository, request Request) (PolicyInput, error) {
	input := PolicyInput{Quality: policy.EffectiveQuality(repo.Config.Quality), Modules: []PolicyModuleInput{}, Files: []SourceInput{}, Declarations: []PolicyDeclarationInput{}}
	for _, active := range repo.Config.ActivePolicyModules {
		input.Modules = append(input.Modules, PolicyModuleInput{Name: active.Name, Root: active.Root})
	}
	scopeByMember := map[string][]string{}
	for _, scope := range request.Scopes {
		for _, member := range scope.Members {
			scopeByMember[member] = append(scopeByMember[member], scope.Handle)
		}
	}
	for _, entry := range request.Inventory {
		if scopes := scopeByMember[entry.Path]; len(scopes) > 0 {
			input.Files = append(input.Files, sourceInput(entry, scopes))
		}
	}
	declarations, err := policyDeclarations(repo.Config.Scope, request)
	if err != nil {
		return PolicyInput{}, err
	}
	input.Declarations = declarations
	if err := validatePolicyDeclarations(&input, request); err != nil {
		return PolicyInput{}, err
	}
	return input, nil
}

func analysisContextPaths(request Request) []string {
	paths := slices.Clone(request.DiagnosticFiles)
	for _, declaration := range request.Policy.Declarations {
		paths = append(paths, declaration.Inputs...)
	}
	for _, scope := range request.Scopes {
		paths = append(paths, scope.Context...)
	}
	if request.Capability == "architecture" {
		for _, entry := range request.Inventory {
			if entry.Asset {
				paths = append(paths, entry.Path)
			}
		}
	}
	for _, entry := range request.Inventory {
		if entry.Control {
			paths = append(paths, entry.Path)
		}
	}
	return sortedUnique(paths)
}

func validateAnalysisScopes(request Request) error {
	if len(request.Scopes) == 0 || len(request.Scopes) > maximumDiscoveryScopes {
		return expected("scopes", fmt.Sprintf("1 to %d items", maximumDiscoveryScopes))
	}
	validation := analysisScopeValidation{
		inventory: inventoryByPath(request.Inventory), handles: map[string]bool{}, members: map[string]bool{}, provider: request.Provider,
	}
	for index, scope := range request.Scopes {
		if err := validation.validate(scope, index); err != nil {
			return err
		}
	}
	return validateAnalysisScopeSelections(request, validation.members)
}

type analysisScopeValidation struct {
	inventory      map[string]InventoryEntry
	handles        map[string]bool
	members        map[string]bool
	provider       string
	totalDataBytes int
}

func (validation *analysisScopeValidation) validate(scope AnalysisScope, index int) error {
	label := indexed("scopes", index)
	if scope.Handle != fmt.Sprintf("scope-%d", index+1) || validation.handles[scope.Handle] {
		return expected(label+".handle", "its unique engine-issued invocation handle")
	}
	validation.handles[scope.Handle] = true
	if err := validateScopeRoot(scope.Root); err != nil {
		return expected(label+".root", "a contained relative directory")
	}
	if len(scope.Members) == 0 || !allContained(scope.EntryFiles, scope.Members) {
		return expected(label+".members", "at least one source with entryFiles contained within it")
	}
	if err := validation.validateMembers(scope, label); err != nil {
		return err
	}
	if err := validation.validateContext(scope.Context, label); err != nil {
		return err
	}
	return validation.validateData(scope.Data, label)
}

func (validation *analysisScopeValidation) validateMembers(scope AnalysisScope, label string) error {
	for _, member := range scope.Members {
		entry, found := validation.inventory[member]
		if !found || !entry.Source || entry.Language != scope.Language || entry.Owner != validation.provider {
			return expected(label+".members", "provider-owned source paths from inventory")
		}
		validation.members[member] = true
	}
	return nil
}

func (validation analysisScopeValidation) validateContext(contexts []string, label string) error {
	seen := map[string]bool{}
	for index, context := range contexts {
		if _, found := validation.inventory[context]; !found || seen[context] {
			return expected(indexed(label+".context", index), "a unique path from inventory")
		}
		seen[context] = true
	}
	return nil
}

func (validation *analysisScopeValidation) validateData(data json.RawMessage, label string) error {
	canonical, err := canonicalScopeData(data)
	if err != nil {
		return fmt.Errorf("%s.data: %w", label, err)
	}
	validation.totalDataBytes += len(canonical)
	if validation.totalDataBytes > maximumScopeDataBytes {
		return expected(label+".data", "aggregate scope data of at most 2097152 bytes")
	}
	return nil
}

func validateAnalysisScopeSelections(request Request, members map[string]bool) error {
	for _, file := range append(slices.Clone(request.Files), request.DiagnosticFiles...) {
		if !members[file] {
			return expected("diagnosticFiles", "selected paths and diagnostics contained in validated scopes")
		}
	}
	for _, file := range request.WriteFiles {
		if !slices.Contains(request.Files, file) || !members[file] {
			return expected("writeFiles", "selected source paths contained in validated scopes")
		}
	}
	return nil
}

func inventoryByPath(entries []InventoryEntry) map[string]InventoryEntry {
	result := make(map[string]InventoryEntry, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result
}

func pathDirectory(file string) string {
	directory := strings.TrimSuffix(file, "/"+filepath.Base(file))
	if directory == file || directory == "" {
		return "."
	}
	return filepath.ToSlash(directory)
}

func validatedAssetLinkInputs(repo repository.Repository, paths []string) map[string][]byte {
	inputs := map[string][]byte{}
	for _, path := range paths {
		if !repo.IsSymbolicLink(path) {
			continue
		}
		data, link, err := repo.AssetLinkIdentity(path)
		if link && err == nil {
			inputs[path] = data
		}
	}
	return inputs
}

func readContextInput(repo repository.Repository, root *os.Root, path string) ([]byte, error) {
	data, link, err := repo.AssetLinkIdentity(path)
	if err != nil || link {
		return data, err
	}
	return readInput(root, path)
}

func readInput(root *os.Root, path string) ([]byte, error) {
	if err := exactRelativePath(path); err != nil {
		return nil, err
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, fmt.Errorf("provider input %s must be a regular file of at most 16 MiB", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("provider input %s exceeds 16 MiB", path)
	}
	return data, nil
}

func inputDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func verifyAnalysisInputs(repo repository.Repository, request Request, response Response) error {
	if err := verifyInputIdentities(repo, append(slices.Clone(request.Context), response.Inputs...)); err != nil {
		return err
	}
	if response.Status != "operational-failure" {
		required := slices.Clone(request.Files)
		if response.Coverage != nil {
			required = append(required, response.Coverage.Analyzed...)
		}
		for _, selected := range required {
			if !slices.ContainsFunc(response.Inputs, func(input InputFile) bool { return input.Path == selected }) {
				return fmt.Errorf("provider did not record selected input %s", selected)
			}
		}
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	return verifyLocations(root, request, response)
}

func verifyInputIdentities(repo repository.Repository, inputs []InputFile) error {
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, input := range inputs {
		data, err := readContextInput(repo, root, input.Path)
		if err != nil {
			return err
		}
		if inputDigest(data) != input.SHA256 {
			return fmt.Errorf("provider input %s changed or has an invalid identity", input.Path)
		}
	}
	return nil
}

func verifyLocations(root *os.Root, request Request, response Response) error {
	for _, finding := range response.Findings {
		if finding.Path == "repository" {
			continue
		}
		if err := verifyLocation(root, finding.Path, finding.Line, finding.Column, ""); err != nil {
			return err
		}
	}
	if response.Facts == nil {
		return nil
	}
	for _, err := range []error{verifyImportLocations(root, request, response), verifyCommentLocations(root, response.Facts.Comments), verifyLiteralLocations(root, response.Facts.Literals), verifyFunctionLocations(root, response.Facts.Functions), verifyDeadCodeLocations(root, response.Facts.DeadCode)} {
		if err != nil {
			return err
		}
	}
	return nil
}

func verifyLiteralLocations(root *os.Root, facts *[]LiteralFact) error {
	if facts == nil {
		return nil
	}
	for _, fact := range *facts {
		if err := verifyLocation(root, fact.Path, fact.Line, fact.Column, ""); err != nil {
			return err
		}
	}
	return nil
}

func verifyImportLocations(root *os.Root, request Request, response Response) error {
	if response.Facts.Imports == nil {
		return nil
	}
	for _, fact := range *response.Facts.Imports {
		if fact.Resolved != "" && !slices.ContainsFunc(append(slices.Clone(request.Context), response.Inputs...), func(input InputFile) bool { return input.Path == fact.Resolved }) {
			return fmt.Errorf("resolved import %s has no verified input identity", fact.Resolved)
		}
		if err := verifyLocation(root, fact.Path, fact.Line, fact.Column, ""); err != nil {
			return err
		}
	}
	return nil
}

func verifyCommentLocations(root *os.Root, facts *[]CommentFact) error {
	if facts == nil {
		return nil
	}
	for _, fact := range *facts {
		if err := verifyLocation(root, fact.Path, fact.Line, fact.Column, fact.Raw); err != nil {
			return err
		}
	}
	return nil
}

func verifyFunctionLocations(root *os.Root, facts *[]FunctionFact) error {
	if facts == nil {
		return nil
	}
	for _, fact := range *facts {
		if err := verifyLocation(root, fact.Path, fact.Line, fact.Column, ""); err != nil {
			return err
		}
	}
	return nil
}

func verifyDeadCodeLocations(root *os.Root, facts *[]DeadCodeFact) error {
	if facts == nil {
		return nil
	}
	for _, fact := range *facts {
		if err := verifyLocation(root, fact.Path, fact.Line, 1, ""); err != nil {
			return err
		}
		if err := verifyLocation(root, fact.Path, fact.EndLine, 1, ""); err != nil {
			return err
		}
	}
	return nil
}

func verifyLocation(root *os.Root, path string, line, column int, raw string) error {
	data, err := readInput(root, path)
	if err != nil {
		return err
	}
	if line == 0 && column == 0 && raw == "" {
		return nil
	}
	if line <= 0 || column <= 0 || !utf8.Valid(data) {
		return fmt.Errorf("provider location in %s is not a valid UTF-8 source coordinate", path)
	}
	index, err := sourceCoordinateOffset(data, line, column)
	if err != nil {
		return fmt.Errorf("provider location in %s: %w", path, err)
	}
	if raw != "" && !bytes.HasPrefix(data[index:], []byte(raw)) {
		return fmt.Errorf("provider comment does not match original source in %s", path)
	}
	return nil
}

func sourceCoordinateOffset(data []byte, line, column int) (int, error) {
	lines := bytes.Split(data, []byte("\n"))
	if line > len(lines) || column > len(lines[line-1])+1 {
		return 0, errors.New("coordinate is outside source")
	}
	index := column - 1
	if index < len(lines[line-1]) && !utf8.RuneStart(lines[line-1][index]) {
		return 0, errors.New("column splits a UTF-8 character")
	}
	for _, previous := range lines[:line-1] {
		index += len(previous) + 1
	}
	return index, nil
}

func applyEdits(repo repository.Repository, request Request, response Response) error {
	if len(response.Edits) == 0 {
		return nil
	}
	if err := validateEdits(response.Edits, response.Status, request); err != nil {
		return err
	}
	for _, edit := range response.Edits {
		if repo.IsGenerated(edit.Path) || repo.IsData(edit.Path) {
			return errors.New("provider edits cannot target generated source or data")
		}
		data, err := repo.Read(edit.Path)
		if err != nil || !utf8.Valid(data) {
			return fmt.Errorf("provider edit target %s is not valid UTF-8 source", edit.Path)
		}
		if err := repo.ValidateRegularFile(edit.Path); err != nil {
			return err
		}
	}
	for _, edit := range response.Edits {
		if err := repo.WriteRegularFile(edit.Path, []byte(edit.Content)); err != nil {
			return err
		}
	}
	return nil
}

func validateEdits(edits []Edit, status string, request Request) error {
	if len(edits) == 0 {
		return nil
	}
	if request.Capability != "format" {
		return expected("edits", "items only for the format capability")
	}
	if request.Mode != "write" {
		return expected("edits", "items only when mode is write")
	}
	if status != "pass" {
		return expected("edits", "items only when status is pass")
	}
	seen := map[string]bool{}
	for index, edit := range edits {
		label := indexed("edits", index)
		if !slices.Contains(request.WriteFiles, edit.Path) {
			return expected(label+".path", "a path from writeFiles")
		}
		if seen[edit.Path] {
			return expected(label+".path", "a path edited only once")
		}
		if !utf8.ValidString(edit.Content) {
			return expected(label+".content", "valid UTF-8")
		}
		seen[edit.Path] = true
	}
	return nil
}
