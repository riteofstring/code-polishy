package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonVultureCallableFieldsCompleteProjectAnalysis(t *testing.T) {
	sources := map[string]string{
		"src/models.py": `from datetime import datetime
from typing import Callable, TypedDict
class Options(TypedDict):
    supplier: Callable[[], datetime]
    convert: Callable[[int], datetime]
    variadic: Callable[..., datetime]
    unused: str
`,
		"src/service.py": `from models import Options
def consume(options: Options):
    return options["supplier"], options["convert"], options["variadic"]
`,
		"src/unrelated.py": "unread_value = 1\n",
	}
	repo, project, response, output := runTypedDictVulture(t, sources)
	if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 {
		t.Fatalf("project analysis=%+v", response)
	}
	for _, name := range []string{"supplier", "convert", "variadic"} {
		if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Path == "src/models.py" && d.Name == name }) {
			t.Fatalf("consumed field %s reported dead: %+v", name, response.Diagnostics)
		}
	}
	for _, test := range []struct{ path, name string }{{"src/models.py", "unused"}, {"src/unrelated.py", "unread_value"}} {
		if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Path == test.path && d.Name == test.name }) {
			t.Fatalf("lost unconsumed %s: %+v", test.name, response.Diagnostics)
		}
	}
	findings := pythonVultureFindings(repo, project, output)
	if slices.ContainsFunc(findings, func(f policy.Finding) bool { return f.Check == "architecture.pythonFactsCoverage" }) {
		t.Fatalf("project facts unavailable: %+v", findings)
	}
}

func TestPythonVultureInvalidFieldReportsSourceWithoutPartialFindings(t *testing.T) {
	repo, project, response, output := runTypedDictVulture(t, map[string]string{
		"src/models.py":    "from typing import Callable, TypedDict\nclass Options(TypedDict):\n    callback: Callable[[make_type()], str]\n",
		"src/unrelated.py": "unread_value = 1\n",
	})
	if !strings.Contains(response.FactsError, "src/models.py:3:5: TypedDict Options.callback") || len(response.Diagnostics) != 0 {
		t.Fatalf("incomplete project=%+v", response)
	}
	findings := pythonVultureFindings(repo, project, output)
	if len(findings) != 1 || findings[0].Check != "architecture.pythonFactsCoverage" || !strings.Contains(findings[0].Message, "Options.callback") {
		t.Fatalf("coverage diagnostic=%+v", findings)
	}
}
