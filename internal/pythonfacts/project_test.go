package pythonfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAnalyzeProjectSourcesPartitionsAProjectLargerThanOneRequest(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]Input, 0, 1001)
	for index := 1000; index >= 0; index-- {
		source := "import project.module_1000\n# " + strings.Repeat("x", 8500) + "\n"
		inputs = append(inputs, Input{Path: projectSourcePath(index), Source: source})
	}
	if _, err := encodeRequest(normalizedRequest(Request{Sources: inputs})); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("whole-project request error = %v", err)
	}
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Sources) != len(inputs) || len(project.Partitions) < 2 || len(project.Identity) != 64 {
		t.Fatalf("project sources=%d partitions=%d identity=%q", len(project.Sources), len(project.Partitions), project.Identity)
	}
	if project.Sources[0].Path != projectSourcePath(0) || project.Sources[len(project.Sources)-1].Path != projectSourcePath(1000) {
		t.Fatalf("project source order starts at %q and ends at %q", project.Sources[0].Path, project.Sources[len(project.Sources)-1].Path)
	}
	if len(project.Sources[0].Imports) != 1 || project.Sources[0].Imports[0].Module != "project.module_1000" {
		t.Fatalf("cross-partition import fact = %+v", project.Sources[0].Imports)
	}
	count := 0
	for index, partition := range project.Partitions {
		if partition.Index != index+1 || partition.Count <= 0 || len(partition.RequestSHA256) != 64 || len(partition.ResponseSHA256) != 64 {
			t.Fatalf("partition = %+v", partition)
		}
		count += partition.Count
	}
	if count != len(inputs) {
		t.Fatalf("partition source count = %d, want %d", count, len(inputs))
	}
}

func TestProjectSourcePartitioningIsDeterministicAndValidatesTheUnion(t *testing.T) {
	inputs := []Input{
		{Path: "src/c.py", Source: "value = 3\n"},
		{Path: "src/a.py", Source: "value = 1\n"},
		{Path: "src/b.py", Source: "value = 2\n"},
	}
	ordered, err := normalizedProjectSources(inputs)
	if err != nil {
		t.Fatal(err)
	}
	limit := projectPartitionTestLimit(t, ordered[0])
	first, err := partitionProjectSources(ordered, limit)
	if err != nil {
		t.Fatal(err)
	}
	second, err := partitionProjectSources(ordered, limit)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != len(inputs) {
		t.Fatalf("partitions = %#v and %#v", first, second)
	}
	project, err := analyzeProjectSources(t.Context(), "/python", inputs, limit, projectResponse)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selectedValues(project.Sources, func(source Source) string { return source.Path }), []string{"src/a.py", "src/b.py", "src/c.py"}) {
		t.Fatalf("project sources = %+v", project.Sources)
	}
}

func TestAnalyzeProjectSourcesFailsClosedForInvalidPartitionResponses(t *testing.T) {
	inputs := []Input{{Path: "src/a.py", Source: "a = 1\n"}, {Path: "src/b.py", Source: "b = 2\n"}}
	for _, testCase := range []struct {
		name    string
		analyze sourceAnalyzer
	}{
		{name: "missing", analyze: func(_ context.Context, _ string, request Request) (Response, error) {
			response, err := projectResponse(t.Context(), "", request)
			response.Sources = nil
			return response, err
		}},
		{name: "reordered", analyze: func(_ context.Context, _ string, request Request) (Response, error) {
			response, err := projectResponse(t.Context(), "", request)
			slices.Reverse(response.Sources)
			return response, err
		}},
		{name: "stale digest", analyze: func(_ context.Context, _ string, request Request) (Response, error) {
			response, err := projectResponse(t.Context(), "", request)
			response.Sources[0].SHA256 = strings.Repeat("0", 64)
			return response, err
		}},
		{name: "adapter failure", analyze: func(context.Context, string, Request) (Response, error) {
			return Response{}, context.DeadlineExceeded
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := analyzeProjectSources(t.Context(), "/python", inputs, maximumRequestSize, testCase.analyze); err == nil || !strings.Contains(err.Error(), "partition") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProjectSourcesRejectInvalidPathsDuplicatesAndResourceOverruns(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		inputs  []Input
		message string
	}{
		{name: "duplicate", inputs: []Input{{Path: "a.py"}, {Path: "a.py"}}, message: "duplicate"},
		{name: "escaping", inputs: []Input{{Path: "../a.py"}}, message: "invalid source input"},
		{name: "backslash", inputs: []Input{{Path: `src\a.py`}}, message: "invalid source input"},
		{name: "oversized source", inputs: []Input{{Path: "a.py", Source: strings.Repeat("x", maximumSourceSize+1)}}, message: "per-source"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := normalizedProjectSources(testCase.inputs); err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func projectSourcePath(index int) string {
	return fmt.Sprintf("src/project/module_%04d.py", index)
}

func projectPartitionTestLimit(t *testing.T, input Input) int {
	t.Helper()
	empty, err := json.Marshal(normalizedRequest(Request{}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return len(empty) + len(item)
}

func projectResponse(_ context.Context, _ string, request Request) (Response, error) {
	response := Response{
		Protocol: Protocol, PythonVersion: "3.12.0", PackagingVersion: PackagingVersion,
		Manifests: []Manifest{}, Locks: []Lock{}, Metadata: []Distribution{}, Sources: []Source{},
		Requirements: []Requirement{}, Specifiers: []Specifier{}, Names: []Name{},
	}
	for _, input := range request.Sources {
		digest := sha256.Sum256([]byte(input.Source))
		response.Sources = append(response.Sources, Source{Path: input.Path, SHA256: hex.EncodeToString(digest[:]), Imports: []Import{}, ComputedImports: []ComputedImport{}})
	}
	return response, nil
}
