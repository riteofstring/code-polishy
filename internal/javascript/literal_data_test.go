package javascript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiteralDataParseRejectsExecutionAndPreservesEveryInput(t *testing.T) {
	root := t.TempDir()
	cases := map[string]struct {
		source string
		valid  bool
	}{
		"default.js":      {`export default {name: "snapshot", count: -2, flags: [true, false, null]};`, true},
		"binding.js":      {`const catalog = {"worksheets": [], "items": []}; export default catalog;`, true},
		"named.mjs":       {`export const catalog = [{name: "one", value: 1.5}];`, true},
		"malformed.js":    {`export default {`, false},
		"import.js":       {`import value from "./default.js"; export default value;`, false},
		"call.js":         {`export default JSON.parse("{}");`, false},
		"getter.js":       {`export default {get value() {return 1}};`, false},
		"spread.js":       {`export default {...{value: 1}};`, false},
		"computed.js":     {`export default {["value"]: 1};`, false},
		"shorthand.js":    {`export default {value};`, false},
		"prototype.js":    {`export default {__proto__: null};`, false},
		"duplicate.js":    {`export default {value: 1, "value": 2};`, false},
		"statement.js":    {`console.log("execute"); export default 1;`, false},
		"assertion.js":    {`export default 1 as number;`, false},
		"hole.js":         {`export default [,1];`, false},
		"binding-type.js": {`const value: number = 1; export default value;`, false},
		"mutable.js":      {`let value = 1; export default value;`, false},
		"deep.js":         {"export default " + strings.Repeat("[", 102) + "1" + strings.Repeat("]", 102) + ";", false},
	}
	paths := []string{}
	for path, entry := range cases {
		paths = append(paths, path)
		if err := os.WriteFile(filepath.Join(root, path), []byte(entry.source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	policyRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Bundle{PolicyRoot: policyRoot}).Parse(t.Context(), root, paths)
	if err != nil {
		t.Fatal(err)
	}
	rejected := map[string]bool{}
	for _, entry := range result.Unsupported {
		rejected[entry.Path] = true
	}
	for path, entry := range cases {
		if rejected[path] == entry.valid {
			t.Errorf("%s validation incorrect: %+v", path, result)
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(data) != entry.source {
			t.Errorf("parse changed %s: %v", path, err)
		}
	}
}
