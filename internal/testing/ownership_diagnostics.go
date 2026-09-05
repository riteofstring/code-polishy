package testing

import (
	"encoding/json"
	"fmt"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type testOwnerCandidate struct {
	Module    string                   `json:"module"`
	Evidence  []policy.FindingLocation `json:"evidence"`
	Ownership *policy.TestOwnership    `json:"ownership,omitempty"`
}

func OwnershipImportSelection(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if !repo.IsExecutableSource(path) || !repo.IsTest(path) || len(repo.TestOwnerships(path)) != 0 {
			continue
		}
		if _, expected := testOwnerCandidates(repo, files, path, nil); expected == "" {
			selected = append(selected, path)
		}
	}
	return selected
}

func EnrichOwnershipFindings(repo repository.Repository, files []string, findings []policy.Finding, imports map[string][]string) []policy.Finding {
	result := slices.Clone(findings)
	for index, finding := range result {
		if finding.Check == "policy.testOwnership" && finding.Subject == "unmapped" {
			result[index] = unmappedTestFinding(repo, files, finding.Path, imports[finding.Path])
		}
	}
	return result
}

func unmappedTestFinding(repo repository.Repository, files []string, path string, imports []string) policy.Finding {
	finding := testOwnershipFinding(path, "unmapped", "governed executable test has no explicit tests.ownership entry")
	candidates, expected := testOwnerCandidates(repo, files, path, imports)
	finding.Fields = map[string]string{}
	if expected != "" {
		finding.Fields["expectedOwner"] = expected
		finding.Message += fmt.Sprintf("; available evidence identifies production module %q", expected)
	} else {
		finding.Message += "; ownership is ambiguous or lacks sufficient evidence"
	}
	const maximumCandidates = 8
	if len(candidates) > maximumCandidates {
		finding.Fields["omittedCandidates"] = fmt.Sprint(len(candidates) - maximumCandidates)
		candidates = candidates[:maximumCandidates]
	}
	for index := range candidates {
		candidate := &candidates[index]
		if suite := compatibleOwnerSuite(repo.Config, candidate.Module, path); suite != "" {
			candidate.Ownership = &policy.TestOwnership{Paths: []string{path}, Module: candidate.Module, FocusedSuite: suite}
		}
		finding.Related = append(finding.Related, candidate.Evidence...)
	}
	if expected != "" && len(candidates) == 1 && candidates[0].Ownership != nil {
		finding.Remediation.Configuration, _ = json.Marshal(candidates[0].Ownership)
	} else {
		finding.Remediation.Configuration, _ = json.Marshal(struct {
			Alternatives []testOwnerCandidate `json:"alternatives"`
		}{Alternatives: candidates})
	}
	return finding
}

func testOwnerCandidates(repo repository.Repository, files []string, path string, imports []string) ([]testOwnerCandidate, string) {
	paired := pairedProductionPaths(path)
	existing := map[string]bool{}
	for _, file := range files {
		existing[file] = true
	}
	paired = slices.DeleteFunc(paired, func(path string) bool { return !existing[path] })
	candidates := productionOwnerEvidence(repo, paired, "paired production file")
	if len(candidates) == 1 {
		return candidates, candidates[0].Module
	}
	contained := []testOwnerCandidate{}
	for _, module := range repo.ModuleNames(path) {
		contained = append(contained, testOwnerCandidate{Module: module, Evidence: []policy.FindingLocation{{Path: path, Message: "test path is contained by module " + module}}})
	}
	if len(contained) == 1 {
		return contained, contained[0].Module
	}
	imported := productionOwnerEvidence(repo, imports, "resolved production import")
	if len(imported) == 1 {
		return imported, imported[0].Module
	}
	return mergeTestOwnerCandidates(candidates, contained, imported), ""
}

func productionOwnerEvidence(repo repository.Repository, paths []string, reason string) []testOwnerCandidate {
	candidates := []testOwnerCandidate{}
	for _, path := range paths {
		if !repo.IsExecutableSource(path) || repo.IsTest(path) {
			continue
		}
		for _, module := range repo.ModuleNames(path) {
			candidates = append(candidates, testOwnerCandidate{Module: module, Evidence: []policy.FindingLocation{{Path: path, Message: reason + " owned by " + module}}})
		}
	}
	return mergeTestOwnerCandidates(candidates)
}

func mergeTestOwnerCandidates(groups ...[]testOwnerCandidate) []testOwnerCandidate {
	byModule := map[string][]policy.FindingLocation{}
	for _, candidates := range groups {
		for _, candidate := range candidates {
			byModule[candidate.Module] = append(byModule[candidate.Module], candidate.Evidence...)
		}
	}
	modules := []string{}
	for module := range byModule {
		modules = append(modules, module)
	}
	slices.Sort(modules)
	result := []testOwnerCandidate{}
	for _, module := range modules {
		evidence := byModule[module]
		slices.SortFunc(evidence, func(left, right policy.FindingLocation) int {
			return strings.Compare(left.Path+"\x00"+left.Message, right.Path+"\x00"+right.Message)
		})
		evidence = slices.Compact(evidence)
		if len(evidence) > 8 {
			evidence = evidence[:8]
		}
		result = append(result, testOwnerCandidate{Module: module, Evidence: evidence})
	}
	return result
}

func pairedProductionPaths(path string) []string {
	base := pathpkg.Base(path)
	production := base
	switch {
	case strings.HasSuffix(base, "_test.go"):
		production = strings.TrimSuffix(base, "_test.go") + ".go"
	case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		production = strings.TrimPrefix(base, "test_")
	case strings.HasSuffix(base, "_test.py"):
		production = strings.TrimSuffix(base, "_test.py") + ".py"
	case strings.Contains(base, ".test."):
		production = strings.Replace(base, ".test.", ".", 1)
	case strings.Contains(base, ".spec."):
		production = strings.Replace(base, ".spec.", ".", 1)
	}
	paired := pathpkg.Join(pathpkg.Dir(path), production)
	result := []string{}
	if paired != path {
		result = append(result, paired)
	}
	for _, segment := range []string{"tests", "__tests__"} {
		parts := strings.Split(paired, "/")
		for index := 0; index < len(parts)-1; index++ {
			if parts[index] == segment {
				result = append(result, strings.Join(append(slices.Clone(parts[:index]), parts[index+1:]...), "/"))
			}
		}
	}
	return result
}
