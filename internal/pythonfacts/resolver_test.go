package pythonfacts

import (
	"bytes"
	"encoding/json"
	"testing"
)

const reachabilityTestMain = `import json,sys
from reachability import resolve_reachability
payload=json.loads(sys.stdin.buffer.read())
evidence,problems=resolve_reachability(payload["modules"],payload["requests"],set())
sys.stdout.write(json.dumps({"evidence":evidence,"problems":problems}))
`

const typedDictFieldsTestMain = `import json,sys
from type_resolver import typed_dict_fields
try:
    sys.stdout.write(json.dumps(typed_dict_fields(json.loads(sys.stdin.buffer.read()))))
except (ValueError, TypeError) as error:
    sys.stderr.write(str(error)+"\n")
    raise SystemExit(2) from None
`

type reachabilityTestResult struct {
	Evidence []ReachabilityEvidence `json:"evidence"`
	Problems []ReachabilityProblem  `json:"problems"`
}

type typedDictTestField struct {
	Path     string `json:"path"`
	TypeName string `json:"typeName"`
	Key      string `json:"key"`
	Line     int    `json:"line"`
}

func resolveReachabilityForTest(t *testing.T, modules []TypeModule, requests []ReachabilityInput) (reachabilityTestResult, error) {
	t.Helper()
	var result reachabilityTestResult
	err := runResolverTestProgram(t, ReachabilitySupportSource()+embeddedPythonModule("__main__", reachabilityTestMain), map[string]any{"modules": modules, "requests": requests}, &result)
	return result, err
}

func resolveTypedDictFieldsForTest(t *testing.T, modules []TypeModule) ([]typedDictTestField, error) {
	t.Helper()
	var fields []typedDictTestField
	err := runResolverTestProgram(t, embeddedPythonModule("__main__", typedDictFieldsTestMain), modules, &fields)
	return fields, err
}

func runResolverTestProgram(t *testing.T, main string, payload, result any) error {
	t.Helper()
	input, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	data, err := runFactProject(t.Context(), typeTestInterpreter(t), bytes.NewReader(input), TypeSupportSource()+main)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, result); err != nil {
		t.Fatal(err)
	}
	return nil
}
