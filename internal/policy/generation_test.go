package policy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestGenerationConfigurationPreservesCommandsAndDefaultsWithoutScheduling(t *testing.T) {
	t.Parallel()
	config, err := Parse(generationConfiguration(t, validGenerationDeclaration()), "")
	if err != nil {
		t.Fatal(err)
	}
	producer := config.Generation.Producers[0]
	if producer.Name != "contracts" || producer.Generate.Cwd != "." || producer.Verify.TimeoutSeconds != 900 || len(producer.Inputs) != 1 {
		t.Fatalf("producer = %+v", producer)
	}
	baseline, err := Parse([]byte(minimalConfig()), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Checks, baseline.Checks) || !reflect.DeepEqual(config.Tests.Suites, baseline.Tests.Suites) {
		t.Fatalf("declaration scheduled a command: %+v, %+v", config.Checks, config.Tests.Suites)
	}
}

func TestGenerationConfigurationRejectsUnsafeAndUnboundedDeclarations(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(map[string]any){
		"missing verify":     func(value map[string]any) { delete(value, "verify") },
		"unknown field":      func(value map[string]any) { value["execute"] = true },
		"empty outputs":      func(value map[string]any) { value["outputs"] = []string{} },
		"universal output":   func(value map[string]any) { value["outputs"] = []string{"**/*.go"} },
		"escaping input":     func(value map[string]any) { value["inputs"] = []string{"../source/**"} },
		"noncanonical input": func(value map[string]any) { value["inputs"] = []string{"src/./contracts/**"} },
		"ambiguous glob":     func(value map[string]any) { value["inputs"] = []string{"src/[ab].go"} },
		"duplicate patterns": func(value map[string]any) { value["outputs"] = []string{"generated/value.go", "generated/value.go"} },
		"too many patterns": func(value map[string]any) {
			value["inputs"] = make([]string, MaximumGenerationPatterns+1)
		},
		"shell evaluation": func(value map[string]any) {
			value["generate"] = map[string]any{"argv": []string{"sh", "-c", "generate"}}
		},
		"escaping executable": func(value map[string]any) { value["generate"] = map[string]any{"argv": []string{"../generate"}} },
		"absolute executable": func(value map[string]any) { value["generate"] = map[string]any{"argv": []string{"C:/generate.exe"}} },
		"escaping cwd": func(value map[string]any) {
			value["generate"] = map[string]any{"argv": []string{"generate"}, "cwd": "../outside"}
		},
		"absolute cwd": func(value map[string]any) {
			value["verify"] = map[string]any{"argv": []string{"verify"}, "cwd": "C:/outside"}
		},
		"secret environment value": func(value map[string]any) {
			value["verify"] = map[string]any{"argv": []string{"verify"}, "environment": []string{"TOKEN=value"}}
		},
		"invalid timeout": func(value map[string]any) {
			value["verify"] = map[string]any{"argv": []string{"verify"}, "timeoutSeconds": 3601}
		},
		"empty argv": func(value map[string]any) { value["verify"] = map[string]any{"argv": []string{}} },
		"unknown command field": func(value map[string]any) {
			value["verify"] = map[string]any{"argv": []string{"verify"}, "runOn": []string{"check"}}
		},
		"oversized argument": func(value map[string]any) {
			value["verify"] = map[string]any{"argv": []string{"verify", strings.Repeat("x", 4097)}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := validGenerationDeclaration()
			change(value)
			if _, err := Parse(generationConfiguration(t, value), ""); err == nil {
				t.Fatal("invalid generation declaration passed")
			}
		})
	}
}

func TestGenerationRuntimeValidationRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()
	for _, environment := range [][]string{{"TOKEN=value"}, {"TOKEN", "TOKEN"}, {strings.Repeat("X", 4097)}, make([]string, 65)} {
		config, err := Parse(generationConfiguration(t, validGenerationDeclaration()), "")
		if err != nil {
			t.Fatal(err)
		}
		config.Generation.Producers[0].Generate.Environment = environment
		if err := ValidateGeneration(config.Generation); err == nil {
			t.Fatal("invalid runtime environment declaration passed")
		}
	}
}

func TestGenerationConfigurationRejectsDuplicateAndOversizedInventories(t *testing.T) {
	t.Parallel()
	for _, count := range []int{2, MaximumGenerationProducers + 1} {
		producers := make([]any, count)
		for index := range producers {
			producers[index] = validGenerationDeclaration()
		}
		if _, err := Parse(generationConfiguration(t, producers...), ""); err == nil {
			t.Fatalf("%d repeated producers passed", count)
		}
	}
}

func validGenerationDeclaration() map[string]any {
	return map[string]any{
		"name": "contracts", "outputs": []string{"generated/contracts.ts"}, "inputs": []string{"src/contracts/**"},
		"generate": map[string]any{"argv": []string{"pnpm", "run", "contracts:generate"}},
		"verify":   map[string]any{"argv": []string{"pnpm", "run", "contracts:verify"}},
	}
}

func generationConfiguration(t *testing.T, producers ...any) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(minimalConfig()), &value); err != nil {
		t.Fatal(err)
	}
	value["generation"] = map[string]any{"producers": producers}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
