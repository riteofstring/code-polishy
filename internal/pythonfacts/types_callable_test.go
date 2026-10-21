package pythonfacts

import (
	"strings"
	"testing"
)

func TestTypedDictCallableFieldFormsAreAccepted(t *testing.T) {
	for _, test := range []struct{ name, imports, annotation string }{
		{"no arguments", "from typing import Callable", "Callable[[], str]"},
		{"typed arguments", "from typing import Callable", "Callable[[int, str], bool]"},
		{"ellipsis", "from typing import Callable", "Callable[..., int]"},
		{"import alias", "from typing import Callable as Function", "Function[[int], str]"},
		{"local alias", "from typing import Callable\nFunction = Callable", "Function[[], str]"},
		{"re-export", "from callbacks import Function", "Function[[], str]"},
		{"parameter specification", "from typing import Callable, ParamSpec\nP = ParamSpec('P')", "Callable[P, str]"},
		{"collections alias", "from collections.abc import Callable as Function", "Function[[int], str]"},
		{"qualified", "import collections.abc", "collections.abc.Callable[..., str]"},
		{"nested", "from typing import Callable, NotRequired", "NotRequired[Callable[[Callable[[], int]], str | None]]"},
		{"functional", "from typing import Callable", "Callable[[], str]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := "class Options(TypedDict):\n    callback: " + test.annotation + "\n    unused: int\n"
			if test.name == "functional" {
				definition = "Options = TypedDict(\"Options\", {\"callback\": " + test.annotation + ", \"unused\": int})\n"
			}
			fields, err := resolveTypeTestSources(t, map[string]string{
				"src/models.py":    "from typing import TypedDict\n" + test.imports + "\n" + definition,
				"src/callbacks.py": "from typing import Callable as Function\n",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields[0].Path != "src/models.py" || fields[0].TypeName != "Options" || fields[0].Key != "callback" {
				t.Fatalf("fields=%+v", fields)
			}
		})
	}
}

func TestTypedDictFieldErrorsIdentifyTheDeclaration(t *testing.T) {
	for _, test := range []struct{ name, fields, reason string }{
		{"unsupported call", "    callback: make_type()\n", "unsupported field type"},
		{"bare ellipsis", "    callback: ...\n", "unsupported field type"},
		{"missing return", "    callback: Callable[[]]\n", "unsupported field type"},
		{"bare list", "    callback: [int]\n", "unsupported field type"},
		{"unknown generic", "    callback: Unknown[[int], str]\n", "unsupported field type"},
		{"unsupported parameter", "    callback: Callable[[make_type()], str]\n", "unsupported field type"},
		{"unsupported result", "    callback: Callable[[int], make_type()]\n", "unsupported field type"},
		{"duplicate", "    callback: str\n    callback: int\n", "duplicate keys"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields, err := resolveTypeTestSources(t, map[string]string{"src/models.py": "from typing import Callable, TypedDict\nclass Options(TypedDict):\n" + test.fields})
			if err == nil {
				t.Fatalf("fields=%+v", fields)
			}
			location := "src/models.py:3:5"
			if test.name == "duplicate" {
				location = "src/models.py:4:5"
			}
			if strings.Contains(err.Error(), "Traceback") {
				t.Fatalf("expected a source diagnostic: %v", err)
			}
			for _, want := range []string{location, "Options.callback", test.reason} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q: %v", want, err)
				}
			}
		})
	}
}
