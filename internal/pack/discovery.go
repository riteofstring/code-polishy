package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const maximumInventoryEntries = 100000
const maximumDiscoveryScopes = 10000
const maximumScopeBytes = 64 << 10
const maximumScopeDataBytes = 2 << 20

func prepareDiscoveryRequest(repo repository.Repository, request Request, command policy.Command, paths []string) (Request, error) {
	request.Operation = "discover"
	request.Inventory = governedInventory(repo, command, paths, request.Profile)
	if len(request.Inventory) > maximumInventoryEntries {
		return Request{}, fmt.Errorf("pack inventory exceeds %d entries", maximumInventoryEntries)
	}
	request.Scopes = []AnalysisScope{}
	request.DiagnosticFiles = []string{}
	request.WriteFiles = []string{}
	policyValue, err := policyInput(repo, request)
	if err != nil {
		return Request{}, err
	}
	request.Policy = policyValue
	contextPaths := []string{}
	for _, entry := range request.Inventory {
		if entry.Metadata || entry.Dependency || entry.Control || slices.Contains(request.Files, entry.Path) {
			contextPaths = append(contextPaths, entry.Path)
		}
	}
	if err := prepareContext(repo, &request, contextPaths); err != nil {
		return Request{}, err
	}
	return request, nil
}

func prepareContext(repo repository.Repository, request *Request, paths []string) error {
	paths = sortedUnique(paths)
	if len(paths) > maximumInventoryEntries {
		return fmt.Errorf("provider context exceeds %d files", maximumInventoryEntries)
	}
	assetLinks := validatedAssetLinkInputs(repo, paths)
	request.Context = make([]InputFile, 0, len(paths))
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, candidate := range paths {
		data, found := assetLinks[candidate]
		if !found {
			data, err = readContextInput(repo, root, candidate)
			if err != nil {
				return err
			}
		}
		request.Context = append(request.Context, InputFile{Path: candidate, SHA256: inputDigest(data)})
	}
	return nil
}

func discoveryRequest(ctx context.Context, repo repository.Repository, command policy.Command, commandRunner runner.Runner, request Request) (Request, Response, error) {
	paths, err := repo.AllFiles()
	if err != nil {
		return Request{}, Response{}, err
	}
	return discoveryRequestPaths(ctx, repo, command, commandRunner, request, paths)
}

func discoveryRequestPaths(ctx context.Context, repo repository.Repository, command policy.Command, commandRunner runner.Runner, request Request, paths []string) (Request, Response, error) {
	discovery, err := prepareDiscoveryRequest(repo, request, command, paths)
	if err != nil {
		return Request{}, Response{}, err
	}
	response, err := execute(ctx, command.Adapter.PackRoot, command, commandRunner, discovery)
	if err != nil {
		return Request{}, Response{}, err
	}
	if response.Status == "operational-failure" {
		return Request{}, Response{}, errors.New(response.Failure)
	}
	if err := verifyDiscoveryInputs(repo, discovery, response); err != nil {
		return Request{}, Response{}, err
	}
	capability, err := capabilityRequest(request, response)
	if err != nil {
		return Request{}, Response{}, err
	}
	return capability, response, nil
}

func validateDiscoveryResponse(response Response, request Request) error {
	if response.Status == "operational-failure" {
		if response.Discovery != nil || response.Coverage != nil || response.Facts != nil || len(response.Findings) != 0 || len(response.Edits) != 0 || len(response.ScopeHandles) != 0 {
			return errors.New("discovery operational failure cannot assert scopes, capability coverage, facts, findings, edits, or scope handles")
		}
		return validateInputs(response.Inputs)
	}
	if response.Discovery == nil {
		return expected("discovery", "a bounded scope result")
	}
	if response.Status != "pass" {
		return expected("status", "pass or operational-failure for discovery")
	}
	if len(response.Evidence) == 0 {
		return expected("evidence", "at least one item when discovery passes")
	}
	if response.Coverage != nil || response.Facts != nil || len(response.Findings) != 0 || len(response.Edits) != 0 || len(response.ScopeHandles) != 0 {
		return errors.New("discovery cannot assert capability coverage, facts, findings, edits, or scope handles")
	}
	if err := validateDiscoveredScopes(response.Discovery.Scopes, request); err != nil {
		return err
	}
	return validateInputs(response.Inputs)
}

func validateDiscoveredScopes(scopes []DiscoveredScope, request Request) error {
	if len(scopes) == 0 || len(scopes) > maximumDiscoveryScopes {
		return expected("discovery.scopes", fmt.Sprintf("1 to %d items", maximumDiscoveryScopes))
	}
	inventory := inventoryByPath(request.Inventory)
	languages := discoveryLanguages(request)
	selected := map[string]int{}
	identities := map[string]bool{}
	totalBytes := 0
	for index := range scopes {
		label := indexed("discovery.scopes", index)
		scope := &scopes[index]
		if strings.TrimSpace(scope.ID) == "" || len(scope.ID) > 256 || identities[scope.ID] {
			return expected(label+".id", "a unique 1 to 256 byte non-whitespace identity")
		}
		identities[scope.ID] = true
		if !languages[scope.Language] {
			return expected(label+".language", "a language declared by this command")
		}
		if err := validateScopeRoot(scope.Root); err != nil {
			return expected(label+".root", "a contained relative directory")
		}
		if len(scope.Members) == 0 {
			return expected(label+".members", "at least one governed source path")
		}
		if scope.EntryFiles == nil || scope.Context == nil || scope.Selected == nil {
			return expected(label, "explicit entryFiles, context, and selected arrays")
		}
		for _, field := range []struct {
			name   string
			values []string
		}{
			{"members", scope.Members}, {"entryFiles", scope.EntryFiles}, {"context", scope.Context}, {"selected", scope.Selected},
		} {
			if err := validateScopePaths(label+"."+field.name, field.values, inventory); err != nil {
				return err
			}
		}
		for _, member := range scope.Members {
			entry := inventory[member]
			if !entry.Source || entry.Language != scope.Language || entry.Owner != request.Provider {
				return expected(label+".members", "source paths owned by this provider and language")
			}
		}
		if !allContained(scope.EntryFiles, scope.Members) {
			return expected(label+".entryFiles", "paths from members")
		}
		if !allContained(scope.Selected, scope.Members) {
			return expected(label+".selected", "paths from members")
		}
		for _, file := range scope.Selected {
			selected[file]++
		}
		canonical, err := canonicalScopeData(scope.Data)
		if err != nil {
			return fmt.Errorf("%s.data: %w", label, err)
		}
		totalBytes += len(canonical)
		if len(canonical) > maximumScopeBytes || totalBytes > maximumScopeDataBytes {
			return expected(label+".data", "bounded canonical JSON")
		}
	}
	for _, file := range request.Files {
		if selected[file] != 1 {
			return expected("discovery.scopes[].selected", fmt.Sprintf("requested source %q exactly once", file))
		}
	}
	return nil
}

func capabilityRequest(request Request, response Response) (Request, error) {
	request.Operation = "check"
	if request.Capability == "format" {
		request.Operation = "format"
	}
	active := map[string]bool{}
	for _, scope := range response.Discovery.Scopes {
		if len(scope.Selected) > 0 {
			active[scope.ID] = true
		}
	}
	includeAll := request.Capability == "architecture"
	request.Files = []string{}
	request.Scopes = []AnalysisScope{}
	for _, discovered := range response.Discovery.Scopes {
		request.Files = append(request.Files, discovered.Selected...)
		if !includeAll && !active[discovered.ID] {
			continue
		}
		canonical, err := canonicalScopeData(discovered.Data)
		if err != nil {
			return Request{}, err
		}
		request.Scopes = append(request.Scopes, AnalysisScope{
			Handle: fmt.Sprintf("scope-%d", len(request.Scopes)+1), Language: discovered.Language, Root: discovered.Root,
			Members: slices.Clone(discovered.Members), EntryFiles: slices.Clone(discovered.EntryFiles), Context: slices.Clone(discovered.Context), Data: canonical,
		})
	}
	request.Files = sortedUnique(request.Files)
	if len(request.Files) == 0 {
		return request, nil
	}
	request.DiagnosticFiles = slices.Clone(request.Files)
	if slices.Contains([]string{"typecheck", "dead-code", "architecture"}, request.Capability) {
		for _, scope := range request.Scopes {
			request.DiagnosticFiles = append(request.DiagnosticFiles, scope.Members...)
		}
		request.DiagnosticFiles = sortedUnique(request.DiagnosticFiles)
	}
	request.WriteFiles = []string{}
	if request.Capability == "format" && request.Mode == "write" {
		inventory := inventoryByPath(request.Inventory)
		for _, file := range request.Files {
			entry := inventory[file]
			if !entry.Generated && !entry.Data {
				request.WriteFiles = append(request.WriteFiles, file)
			}
		}
	}
	return request, nil
}

func verifyDiscoveryInputs(repo repository.Repository, request Request, response Response) error {
	allowed := map[string]bool{}
	for _, input := range request.Context {
		allowed[input.Path] = true
	}
	for index, input := range response.Inputs {
		if !allowed[input.Path] {
			return expected(indexed("inputs", index)+".path", "a path from discovery context")
		}
	}
	return verifyInputIdentities(repo, append(slices.Clone(request.Context), response.Inputs...))
}

func validateScopePaths(label string, values []string, inventory map[string]InventoryEntry) error {
	seen := map[string]bool{}
	for index, value := range values {
		if _, found := inventory[value]; !found || seen[value] {
			return expected(indexed(label, index), "a unique path from inventory")
		}
		seen[value] = true
	}
	return nil
}

func validateScopeRoot(root string) error {
	if root == "." {
		return nil
	}
	if err := exactRelativePath(root); err != nil || path.Clean(root) != root {
		return errors.New("invalid root")
	}
	return nil
}

func allContained(values, allowed []string) bool {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}

func discoveryLanguages(request Request) map[string]bool {
	result := map[string]bool{}
	for _, entry := range request.Inventory {
		if entry.Language != "" {
			result[entry.Language] = true
		}
	}
	return result
}

func canonicalScopeData(data json.RawMessage) (json.RawMessage, error) {
	if len(data) == 0 || len(data) > maximumScopeBytes {
		return nil, errors.New("expected 1 to 65536 bytes of JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("expected valid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected exactly one JSON value")
	}
	if value == nil || jsonDepth(value) > 32 {
		return nil, errors.New("expected non-null JSON with depth at most 32")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("expected canonicalizable JSON")
	}
	return canonical, nil
}

func jsonDepth(value any) int {
	switch typed := value.(type) {
	case []any:
		depth := 1
		for _, child := range typed {
			depth = max(depth, 1+jsonDepth(child))
		}
		return depth
	case map[string]any:
		depth := 1
		for _, child := range typed {
			depth = max(depth, 1+jsonDepth(child))
		}
		return depth
	default:
		return 1
	}
}
