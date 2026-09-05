package quality

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDeclaredDataModulesRejectExecutableContentWithoutRunningIt(t *testing.T) {
	repo := qualityRepository(t)
	var err error
	repo.PolicyRoot, err = filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(repo.Root, "executed")
	sources := map[string]string{
		"valid.js":       "const catalog = {  \"names\": [\"Olá\"], \"count\": -2 };\nexport default catalog;\n",
		"valid.mjs":      "export default [ 1, true, null ];\n",
		"call.js":        "export default JSON.parse('{}');\n",
		"getter.js":      "export default {get value() {return 1}};\n",
		"computed.js":    "export default {[globalThis.key]: 1};\n",
		"spread.js":      "export default {...globalThis.value};\n",
		"template.js":    "export default `${globalThis.value}`;\n",
		"mutable.js":     "let value = {}; export default value;\n",
		"reassigned.js":  "const value = {}; value.a = 1; export default value;\n",
		"named.js":       "export const value = {}; export default value;\n",
		"other.js":       "const value = {}; export default other;\n",
		"function.js":    "export default function() {};\n",
		"invalid.js":     "const = ; export default {};\n",
		"side-effect.js": "import {writeFileSync} from 'node:fs'; writeFileSync(" + strconv.Quote(sentinel) + ", 'executed'); export default {};\n",
	}
	paths := []string{}
	for path, text := range sources {
		writeQualityFile(t, repo.Root, path, text)
		paths = append(paths, path)
	}
	repo.Config.Scope.Data = paths
	findings := DataSyntaxFindings(t.Context(), repo, paths)
	if len(findings) != len(sources)-2 {
		t.Fatalf("data module validation = %+v", findings)
	}
	for _, finding := range findings {
		if finding.Check != "quality.dataSyntax" || finding.Path == "valid.js" || finding.Path == "valid.mjs" {
			t.Fatalf("unexpected finding: %+v", finding)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("data validation executed source: %v", err)
	}
	if findings := JavaScriptFormatWrite(t.Context(), repo, paths); len(findings) != 0 {
		t.Fatalf("format data modules: %+v", findings)
	}
	for path, before := range sources {
		after, err := os.ReadFile(filepath.Join(repo.Root, path))
		if err != nil || string(after) != before {
			t.Fatalf("data bytes changed in %s: %v", path, err)
		}
	}
}
