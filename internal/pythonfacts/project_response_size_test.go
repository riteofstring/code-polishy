package pythonfacts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestProjectSourcesRetriesOversizedResponsesWithoutLosingFacts(t *testing.T) {
	inputs := []Input{{Path: "a.py", Source: "a=1"}, {Path: "b.py", Source: "b=2"}, {Path: "c.py", Source: "c=3"}, {Path: "d.py", Source: "d=4"}, {Path: "e.py", Source: "e=5"}}
	expected, err := analyzeProjectSources(t.Context(), "/python", inputs, maximumRequestSize, projectResponse)
	if err != nil {
		t.Fatal(err)
	}
	analyzer := func(ctx context.Context, python string, request Request) (Response, error) {
		if len(request.Sources) > 1 {
			return Response{}, errResponseTooLarge
		}
		return projectResponse(ctx, python, request)
	}
	actual, err := analyzeProjectSources(t.Context(), "/python", inputs, maximumRequestSize, analyzer)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Sources, expected.Sources) || actual.Identity != expected.Identity || len(actual.Partitions) != len(inputs) {
		t.Fatalf("sources=%+v partitions=%+v", actual.Sources, actual.Partitions)
	}
	assertProjectPartitionCoverage(t, actual, len(inputs))
}

func TestProjectSourcesRejectsUnsplittableResponse(t *testing.T) {
	calls := 0
	analyzer := func(context.Context, string, Request) (Response, error) {
		calls++
		return Response{}, errResponseTooLarge
	}
	project, err := analyzeProjectSources(t.Context(), "/python", []Input{{Path: "a.py", Source: "a=1"}}, maximumRequestSize, analyzer)
	if !errors.Is(err, errResponseTooLarge) || len(project.Sources) != 0 || calls != 1 {
		t.Fatalf("project=%+v err=%v calls=%d", project, err, calls)
	}
}

func TestProjectSourcesSplitsRealAdapterResponseOverflow(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for index := 0; index < 6000; index++ {
		fmt.Fprintf(&source, "value_%d = %d\n", index, index)
	}
	inputs := make([]Input, 8)
	for index := range inputs {
		inputs[index] = Input{Path: fmt.Sprintf("module_%d.py", index), Source: source.String()}
	}
	if _, err := analyze(t.Context(), python, Request{Sources: inputs}); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("whole response error=%v", err)
	}
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Partitions) < 2 || len(project.Sources) != len(inputs) {
		t.Fatalf("partitions=%d sources=%d", len(project.Partitions), len(project.Sources))
	}
	for index, input := range inputs {
		single, err := analyze(t.Context(), python, Request{Sources: []Input{input}})
		if err != nil || !reflect.DeepEqual(project.Sources[index], single.Sources[0]) {
			t.Fatalf("source %s lost facts: %v", input.Path, err)
		}
	}
	assertProjectPartitionCoverage(t, project, len(inputs))
}
