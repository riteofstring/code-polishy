package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func prepareInputs(repo repository.Repository, request *Request) error {
	paths, err := repo.AllFiles()
	if err != nil {
		return err
	}
	paths = sortedUnique(append(paths, request.Files...))
	if len(paths) > 10000 {
		return errors.New("provider context exceeds 10000 files")
	}
	request.Context = make([]InputFile, 0, len(paths))
	request.Policy = PolicyInput{Quality: policy.EffectiveQuality(repo.Config.Quality), Files: []SourceInput{}, EntryPoints: slices.Clone(repo.Config.Scope.EntryPoints)}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range paths {
		data, err := readInput(root, path)
		if err != nil {
			return err
		}
		request.Context = append(request.Context, InputFile{Path: path, SHA256: inputDigest(data)})
		request.Policy.Files = append(request.Policy.Files, SourceInput{Path: path, Language: repo.Language(path), Test: repo.IsTest(path), Generated: repo.IsGenerated(path), Development: repo.IsDevelopment(path)})
	}
	return nil
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
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, input := range append(slices.Clone(request.Context), response.Inputs...) {
		data, err := readInput(root, input.Path)
		if err != nil {
			return err
		}
		if inputDigest(data) != input.SHA256 {
			return fmt.Errorf("provider input %s changed or has an invalid identity", input.Path)
		}
	}
	if response.Status != "operational-failure" {
		for _, selected := range request.Files {
			if !slices.ContainsFunc(response.Inputs, func(input InputFile) bool { return input.Path == selected }) {
				return fmt.Errorf("provider did not record selected input %s", selected)
			}
		}
	}
	return verifyLocations(root, response)
}

func verifyLocations(root *os.Root, response Response) error {
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
	for _, err := range []error{verifyImportLocations(root, response), verifyCommentLocations(root, response.Facts.Comments), verifyFunctionLocations(root, response.Facts.Functions)} {
		if err != nil {
			return err
		}
	}
	return nil
}

func verifyImportLocations(root *os.Root, response Response) error {
	if response.Facts.Imports == nil {
		return nil
	}
	for _, fact := range *response.Facts.Imports {
		if fact.Resolved != "" && !slices.ContainsFunc(response.Inputs, func(input InputFile) bool { return input.Path == fact.Resolved }) {
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
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := validateEdits(response.Edits, response.Status, request); err != nil {
		return err
	}
	for _, edit := range response.Edits {
		if repo.IsGenerated(edit.Path) || repo.IsData(edit.Path) {
			return errors.New("provider edits cannot target generated source or data")
		}
	}
	for _, edit := range response.Edits {
		info, err := root.Stat(edit.Path)
		if err != nil {
			return err
		}
		if err := root.WriteFile(edit.Path, []byte(edit.Content), info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func validateEdits(edits []Edit, status string, request Request) error {
	if len(edits) == 0 {
		return nil
	}
	if request.Capability != "format" || request.Mode != "write" || status != "pass" {
		return errors.New("only successful format writes may return edits")
	}
	seen := map[string]bool{}
	for _, edit := range edits {
		if seen[edit.Path] || !slices.Contains(request.Files, edit.Path) || !utf8.ValidString(edit.Content) {
			return errors.New("provider edits must target distinct selected UTF-8 files")
		}
		seen[edit.Path] = true
	}
	return nil
}
