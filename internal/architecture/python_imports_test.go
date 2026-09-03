package architecture

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonImportReferencesParseStaticStatementsAndMaskNonCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		source     string
		references []pythonImportReference
	}{
		{
			name: "aliases relative imports and star imports",
			source: "import alpha.beta as beta, gamma\n" +
				"from .relative import one as renamed, two\n" +
				"from .. import *\n" +
				"from . import current\n",
			references: []pythonImportReference{
				{Module: "alpha.beta", Line: 1, Literal: true},
				{Module: "gamma", Line: 1, Literal: true},
				{Module: ".relative", Names: []string{"one", "two"}, Line: 2, Literal: true},
				{Module: "..", Names: []string{"*"}, Line: 3, Literal: true},
				{Module: ".", Names: []string{"current"}, Line: 4, Literal: true},
			},
		},
		{
			name: "parenthesized imports and CRLF continuations",
			source: "from package.sub import (\r\n" +
				"    first,\r\n" +
				"    second as renamed,\r\n" +
				")\r\n" +
				"import alpha.\\\r\n" +
				"beta as gamma\r\n",
			references: []pythonImportReference{
				{Module: "package.sub", Names: []string{"first", "second"}, Line: 1, Literal: true},
				{Module: "alpha.beta", Line: 5, Literal: true},
			},
		},
		{
			name: "semicolons end individual import statements",
			source: "import first; import second\n" +
				"from third import item; import fourth\n",
			references: []pythonImportReference{
				{Module: "first", Line: 1, Literal: true},
				{Module: "second", Line: 1, Literal: true},
				{Module: "third", Names: []string{"item"}, Line: 2, Literal: true},
				{Module: "fourth", Line: 2, Literal: true},
			},
		},
		{
			name: "comments and strings do not create imports",
			source: `doc = """
import ignored.one
"""
text = "import ignored.two \" still text"
# import ignored.three
import kept
`,
			references: []pythonImportReference{{Module: "kept", Line: 6, Literal: true}},
		},
		{
			name: "escaped strings preserve physical lines and mask escaped triple quotes",
			source: "doc = \"x\\\n" +
				"y\"\n" +
				"import kept\n" +
				`doc = """escaped \""" remains text
import ignored
"""
import also_kept
`,
			references: []pythonImportReference{
				{Module: "kept", Line: 3, Literal: true},
				{Module: "also_kept", Line: 7, Literal: true},
			},
		},
		{
			name: "incomplete statements do not create partial imports",
			source: "from package import\n" +
				"import package as 9\n" +
				"from package import (item\n",
			references: []pythonImportReference{},
		},
		{
			name:       "ASCII identifiers may contain underscores and trailing digits",
			source:     "import _alpha, beta2\n",
			references: []pythonImportReference{{Module: "_alpha", Line: 1, Literal: true}, {Module: "beta2", Line: 1, Literal: true}},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if references := pythonImportReferences([]byte(testCase.source)); !reflect.DeepEqual(references, testCase.references) {
				t.Fatalf("references = %#v, want %#v", references, testCase.references)
			}
		})
	}
}

func TestPythonImportReferencesRecognizeDynamicBindings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		source     string
		references []pythonImportReference
	}{
		{
			name:       "builtin function without an import",
			source:     "__import__(\"domain.model\")\n",
			references: []pythonImportReference{{Module: "domain.model", Line: 1, Literal: true}},
		},
		{
			name: "importlib module binding",
			source: "import importlib\n" +
				"importlib.import_module(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "importlib", Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "module alias with computed import",
			source: "import importlib as loader\n" +
				"loader.import_module(module_name)\n",
			references: []pythonImportReference{
				{Module: "importlib", Line: 1, Literal: true},
				{Line: 2},
			},
		},
		{
			name: "nested importlib binding",
			source: "import importlib.util\n" +
				"importlib.import_module(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "importlib.util", Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "parenthesized function alias",
			source: "from importlib import (\n" +
				"    import_module as load,\n" +
				")\n" +
				"load(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "importlib", Names: []string{"import_module"}, Line: 1, Literal: true},
				{Module: "domain.model", Line: 4, Literal: true},
			},
		},
		{
			name: "builtins module alias accepts raw literals",
			source: "import builtins as builtin\n" +
				"builtin.__import__(r\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "builtins", Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "builtins function alias with computed import",
			source: "from builtins import __import__ as load\n" +
				"load(module_name)\n",
			references: []pythonImportReference{
				{Module: "builtins", Names: []string{"__import__"}, Line: 1, Literal: true},
				{Line: 2},
			},
		},
		{
			name: "star import exposes import module",
			source: "from importlib import *\n" +
				"import_module(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "importlib", Names: []string{"*"}, Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "multiline literal argument",
			source: "importlib.import_module(\n" +
				"    \"domain.model\"\n" +
				")\n",
			references: []pythonImportReference{{Module: "domain.model", Line: 1, Literal: true}},
		},
		{
			name: "unbound lookalikes are not dynamic imports",
			source: `text = "importlib.import_module('domain.model')"
# __import__("domain.model")
loader.import_module("domain.model")
not__import__("domain.model")
import importlib.util as util
util.import_module("domain.model")
`,
			references: []pythonImportReference{{Module: "importlib.util", Line: 5, Literal: true}},
		},
		{
			name:       "function references without calls are ignored",
			source:     "importlib.import_module\n__import__\n",
			references: []pythonImportReference{},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if references := pythonImportReferences([]byte(testCase.source)); !reflect.DeepEqual(references, testCase.references) {
				t.Fatalf("references = %#v, want %#v", references, testCase.references)
			}
		})
	}
}

func TestPythonImportReferencesKeepNestedNamesAndInvalidBindingsExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		source     string
		references []pythonImportReference
	}{
		{
			name: "blank lines and trailing commas preserve parenthesized names",
			source: "from package import (\n" +
				"\n" +
				"    first as renamed,\n" +
				"\n" +
				")\n",
			references: []pythonImportReference{{Module: "package", Names: []string{"first"}, Line: 1, Literal: true}},
		},
		{
			name:       "empty parenthesized imports are rejected",
			source:     "from package import ()\n",
			references: []pythonImportReference{},
		},
		{
			name: "multiple imported names retain an import module binding",
			source: "from importlib import import_module, unrelated\n" +
				"import_module(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "importlib", Names: []string{"import_module", "unrelated"}, Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "builtins star import creates an import binding",
			source: "from builtins import *\n" +
				"__import__(\"domain.model\")\n",
			references: []pythonImportReference{
				{Module: "builtins", Names: []string{"*"}, Line: 1, Literal: true},
				{Module: "domain.model", Line: 2, Literal: true},
			},
		},
		{
			name: "incomplete module aliases do not create dynamic bindings",
			source: "import importlib as\n" +
				"loader.import_module(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
		{
			name: "incomplete function aliases do not create dynamic bindings",
			source: "from importlib import import_module as\n" +
				"load(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
		{
			name: "invalid later module aliases roll back earlier bindings",
			source: "import importlib as load, broken as\n" +
				"load.import_module(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
		{
			name: "trailing module alias commas do not create bindings",
			source: "import importlib as load,\n" +
				"load.import_module(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
		{
			name: "invalid later function aliases roll back earlier bindings",
			source: "from importlib import import_module as load, broken as\n" +
				"load(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
		{
			name: "trailing function alias commas do not create bindings",
			source: "from importlib import import_module as load,\n" +
				"load(\"domain.model\")\n",
			references: []pythonImportReference{},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if references := pythonImportReferences([]byte(testCase.source)); !reflect.DeepEqual(references, testCase.references) {
				t.Fatalf("references = %#v, want %#v", references, testCase.references)
			}
		})
	}
}

func TestPythonDynamicImportReferencesRequireExactBoundariesAndLiterals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		source     string
		references []pythonImportReference
	}{
		{
			name: "supported string prefixes remain literal imports",
			source: "__import__(r\"raw\")\n" +
				"__import__(R\"upper_raw\")\n" +
				"__import__(b\"bytes\")\n" +
				"__import__(B\"upper_bytes\")\n" +
				"__import__(u\"unicode\")\n" +
				"__import__(U\"upper_unicode\")\n",
			references: []pythonImportReference{
				{Module: "raw", Line: 1, Literal: true},
				{Module: "upper_raw", Line: 2, Literal: true},
				{Module: "bytes", Line: 3, Literal: true},
				{Module: "upper_bytes", Line: 4, Literal: true},
				{Module: "unicode", Line: 5, Literal: true},
				{Module: "upper_unicode", Line: 6, Literal: true},
			},
		},
		{
			name: "only exact plain literal arguments are trusted",
			source: "__import__(f\"formatted\")\n" +
				"__import__(fr\"combined\")\n" +
				"__import__(br\"combined\")\n" +
				"__import__(\"domain\" + suffix)\n" +
				"__import__(\"domain\" \".model\")\n" +
				"__import__(\"domain\\\\x2emodel\")\n" +
				"__import__(\"\"\"domain.model\"\"\")\n" +
				"__import__(\"domain.model\", globals(), locals(), (), 1)\n" +
				"importlib.import_module(\".model\", package_name)\n",
			references: []pythonImportReference{
				{Line: 1}, {Line: 2}, {Line: 3}, {Line: 4}, {Line: 5},
				{Line: 6}, {Line: 7}, {Line: 8}, {Line: 9},
			},
		},
		{
			name: "explicit continuations bridge calls but bare newlines do not",
			source: "__import__ \\\n" +
				"(\"continued.call\")\n" +
				"__import__(\\\n" +
				"    \"continued.argument\"\n" +
				")\n" +
				"__import__\n" +
				"(\"separate.expression\")\n",
			references: []pythonImportReference{
				{Module: "continued.call", Line: 1, Literal: true},
				{Module: "continued.argument", Line: 3, Literal: true},
			},
		},
		{
			name: "bare LF and CRLF between callees and parentheses are not calls",
			source: "__import__\n" +
				"(\"separate.lf\")\n" +
				"__import__\r\n" +
				"(\"separate.crlf\")\r\n",
			references: []pythonImportReference{},
		},
		{
			name: "parenthesized known calls stay unproven",
			source: "(__import__)(\"domain.model\")\n" +
				"(importlib.import_module)(\"domain.model\")\n",
			references: []pythonImportReference{{Line: 1}, {Line: 2}},
		},
		{
			name: "only whole dynamic call names are accepted",
			source: "__import__suffix(\"wrong\")\n" +
				".__import__(\"wrong\")\n" +
				"(__import__(\"right\"))\n" +
				"importlib.import_module_suffix(\"wrong\")\n" +
				"importlib.import_module(\"right.two\")\n",
			references: []pythonImportReference{
				{Module: "right", Line: 3, Literal: true},
				{Module: "right.two", Line: 5, Literal: true},
			},
		},
		{
			name:       "calls without literal arguments remain unproven",
			source:     "__import__()\n",
			references: []pythonImportReference{{Line: 1}},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if references := pythonImportReferences([]byte(testCase.source)); !reflect.DeepEqual(references, testCase.references) {
				t.Fatalf("references = %#v, want %#v", references, testCase.references)
			}
		})
	}
}

func TestPythonImportReferencesKeepDynamicCallSpacingAndLiteralsExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		source     string
		references []pythonImportReference
	}{
		{
			name:       "spaces and tabs before an opening parenthesis",
			source:     "__import__ \t(\"space.tab\")\n",
			references: []pythonImportReference{{Module: "space.tab", Line: 1, Literal: true}},
		},
		{
			name:       "CRLF continuation before an opening parenthesis",
			source:     "__import__ \\\r\n(\"continued.callee\")\r\n",
			references: []pythonImportReference{{Module: "continued.callee", Line: 1, Literal: true}},
		},
		{
			name:       "CRLF whitespace around a literal argument",
			source:     "__import__( \t\r\n\"argument.crlf\"\r\n)\r\n",
			references: []pythonImportReference{{Module: "argument.crlf", Line: 1, Literal: true}},
		},
		{
			name:       "CRLF continuations around a literal argument",
			source:     "__import__(\\\r\n\"continued.argument\"\\\r\n)\r\n",
			references: []pythonImportReference{{Module: "continued.argument", Line: 1, Literal: true}},
		},
		{
			name: "single quoted literals remain exact",
			source: "__import__('single.quoted')\n" +
				"__import__(r'raw.single')\n",
			references: []pythonImportReference{
				{Module: "single.quoted", Line: 1, Literal: true},
				{Module: "raw.single", Line: 2, Literal: true},
			},
		},
		{
			name:       "unfinished call is unproven",
			source:     "__import__(",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "single quoted triple literal is unproven",
			source:     "__import__('''triple''')\n",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "unclosed literal is unproven",
			source:     "__import__(\"unclosed",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "literal newline is unproven",
			source:     "__import__(\"line\nbreak\")\n",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "literal carriage return is unproven",
			source:     "__import__(\"carriage\rreturn\")\n",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "escaped raw literal is unproven",
			source:     "__import__(r\"raw\\path\")\n",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "continuation without an opening parenthesis is not a call",
			source:     "__import__ \\\r\n",
			references: []pythonImportReference{},
		},
		{
			name:       "callee ending in a backslash is not a call",
			source:     "__import__\\",
			references: []pythonImportReference{},
		},
		{
			name:       "argument suffix ending in a backslash is unproven",
			source:     "__import__(\"module\"\\",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "incomplete triple string at EOF is masked safely",
			source:     "doc = '''x''",
			references: []pythonImportReference{},
		},
		{
			name:       "incomplete CRLF continuation inside a call is unproven",
			source:     "__import__(\\\r",
			references: []pythonImportReference{{Line: 1}},
		},
		{
			name:       "bare carriage return does not bridge a call",
			source:     "__import__\r(\"separate\")\n",
			references: []pythonImportReference{},
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if references := pythonImportReferences([]byte(testCase.source)); !reflect.DeepEqual(references, testCase.references) {
				t.Fatalf("references = %#v, want %#v", references, testCase.references)
			}
		})
	}
}

func TestPythonTokenImportPathKeepsImportAsAStatementKeyword(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tokens []pythonImportToken
		module string
		next   int
		found  bool
	}{
		{name: "bare keyword", tokens: []pythonImportToken{{Value: "import"}}},
		{name: "single relative level", tokens: []pythonImportToken{{Value: "."}, {Value: "import"}}, module: ".", next: 1, found: true},
		{name: "double relative level", tokens: []pythonImportToken{{Value: "."}, {Value: "."}, {Value: "import"}}, module: "..", next: 2, found: true},
		{name: "dotted keyword", tokens: []pythonImportToken{{Value: "package"}, {Value: "."}, {Value: "import"}}, next: 1},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			module, next, found := pythonTokenImportPath(testCase.tokens, 0)
			if module != testCase.module || next != testCase.next || found != testCase.found {
				t.Fatalf("pythonTokenImportPath() = (%q, %d, %t), want (%q, %d, %t)", module, next, found, testCase.module, testCase.next, testCase.found)
			}
		})
	}
}

func TestPythonArchitectureRecognizesOnlyCompleteStaticImportStatements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "direct import aliases",
			source:  "import domain.model as model\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "named imports",
			source:  "from domain import model\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "bare relative imports",
			source:  "from . import model\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "escaping relative imports",
			source:  "from ... import domain\n",
			message: "relative Python import escapes its project",
		},
		{
			name: "parenthesized names with blank lines and a trailing comma",
			source: "from domain import (\n" +
				"\n" +
				"    model,\n" +
				"\n" +
				")\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:   "incomplete direct aliases",
			source: "import domain.model as\n",
		},
		{
			name:   "empty parenthesized names",
			source: "from domain import ()\n",
		},
		{
			name:   "incomplete parenthesized aliases",
			source: "from domain import (model as,\n)\n",
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			findings := pythonImportArchitectureFindings(t, testCase.source)
			if testCase.message == "" {
				if len(findings) != 0 {
					t.Fatalf("findings = %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" || !strings.Contains(findings[0].Message, testCase.message) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonArchitectureRequiresLiteralDynamicImportEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "single quoted builtin call",
			source:  "__import__('domain.model')\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "raw string builtin call",
			source:  "__import__(r'domain.model')\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "unicode string builtin call",
			source:  "__import__(u\"domain.model\")\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name: "multiline importlib call",
			source: "importlib.import_module(\n" +
				"    'domain.model'\n" +
				")\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name: "aliased importlib call",
			source: "import importlib as loader\n" +
				"loader.import_module('domain.model')\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name: "aliased builtins call",
			source: "from builtins import __import__ as load\n" +
				"load('domain.model')\n",
			message: "Ruff did not resolve local-looking Python import",
		},
		{
			name:    "computed builtin call",
			source:  "__import__(module_name)\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "computed importlib call",
			source:  "importlib.import_module(module_name)\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "missing builtin argument",
			source:  "__import__()\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name: "formatted string call",
			source: "from importlib import import_module as load\n" +
				"load(f'domain.model')\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name: "two character string prefix call",
			source: "from importlib import import_module as load\n" +
				"load(br'domain.model')\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "escaped string call",
			source:  "__import__(\"domain\\\\x2emodel\")\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "adjacent literal call",
			source:  "__import__(\"domain\" \".model\")\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "triple quoted call",
			source:  "__import__(\"\"\"domain.model\"\"\")\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "multi argument builtin call",
			source:  "__import__(\"domain.model\", globals(), locals(), (), 1)\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "multi argument importlib call",
			source:  "importlib.import_module(\".model\", package_name)\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "parenthesized builtin call",
			source:  "(__import__)(\"domain.model\")\n",
			message: "dynamic Python import cannot be proven statically",
		},
		{
			name:    "parenthesized importlib call",
			source:  "(importlib.import_module)(\"domain.model\")\n",
			message: "dynamic Python import cannot be proven statically",
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			findings := pythonImportArchitectureFindings(t, testCase.source)
			if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" || !strings.Contains(findings[0].Message, testCase.message) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonArchitectureRejectsDynamicCallNameLookalikes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
	}{
		{name: "attribute lookup", source: "holder.__import__(\"domain.model\")\n"},
		{name: "importlib suffix", source: "importlib.import_module_suffix(\"domain.model\")\n"},
		{name: "bare newline builtin", source: "__import__\n(\"domain.model\")\n"},
		{name: "bare CRLF importlib", source: "importlib.import_module\r\n(\"domain.model\")\r\n"},
	}
	for _, character := range []string{"_", "a", "z", "A", "Z", "0", "9"} {
		cases = append(cases,
			struct {
				name   string
				source string
			}{name: "identifier prefix " + character, source: "prefix" + character + "__import__(\"domain.model\")\n"},
			struct {
				name   string
				source string
			}{name: "identifier suffix " + character, source: "__import__" + character + "suffix(\"domain.model\")\n"},
		)
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if findings := pythonImportArchitectureFindings(t, testCase.source); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func pythonImportArchitectureFindings(t *testing.T, source string) []policy.Finding {
	t.Helper()
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "domain", Paths: []string{"src/domain/**"}},
		{Name: "web", Paths: []string{"src/web/**"}, DependsOn: []string{"domain"}},
	})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/domain/__init__.py", "")
	writeArchitectureFile(t, repo.Root, "src/domain/model.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/__init__.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/model.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/app.py", source)
	graphRunner := &pythonGraphRunner{outputs: map[string]string{".": `{"src/web/app.py":[]}`}}
	return CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
}

func TestPythonRelativeImportResolutionStaysWithinTheProject(t *testing.T) {
	t.Parallel()
	index := pythonModuleIndex{packageOf: map[string]string{
		"root.py":                             "",
		"src/nested/consumer.py":              "nested",
		"src/nested/deep/consumer.py":         "nested.deep",
		"src/nested/deep/package/__init__.py": "nested.deep.package",
	}}
	cases := []struct {
		name   string
		source string
		module string
		want   string
		valid  bool
	}{
		{name: "absolute import", source: "src/nested/consumer.py", module: "outside", want: "outside", valid: true},
		{name: "same package", source: "src/nested/consumer.py", module: ".sibling", want: "nested.sibling", valid: true},
		{name: "parent package", source: "src/nested/deep/consumer.py", module: "..sibling", want: "nested.sibling", valid: true},
		{name: "project root", source: "src/nested/deep/consumer.py", module: "...sibling", want: "sibling", valid: true},
		{name: "root package", source: "root.py", module: ".", want: "", valid: true},
		{name: "escaping root", source: "root.py", module: "..", valid: false},
		{name: "escaping nested package", source: "src/nested/deep/consumer.py", module: "....sibling", valid: false},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			module, valid := pythonResolvedImportModule(index, testCase.source, testCase.module)
			if module != testCase.want || valid != testCase.valid {
				t.Fatalf("pythonResolvedImportModule(%q, %q) = (%q, %t), want (%q, %t)", testCase.source, testCase.module, module, valid, testCase.want, testCase.valid)
			}
		})
	}
}

func TestPythonImportCoverageRequiresExactLocalGraphEvidence(t *testing.T) {
	t.Parallel()
	index := newPythonModuleIndex(repository.PythonProject{
		Root:       ".",
		SourceRoot: "src",
		Files: []string{
			"shared.py",
			"src/shared.py",
			"src/domain/__init__.py",
			"src/domain/model.py",
			"src/domain/model.pyi",
			"src/namespace/value.py",
			"src/nested/consumer.py",
			"src/web/consumer.py",
			"src/web/helper.py",
		},
	})
	cases := []struct {
		name         string
		source       string
		reference    pythonImportReference
		dependencies map[string]bool
		want         string
	}{
		{
			name:      "computed imports fail closed",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Line: 3},
			want:      "line 3 dynamic Python import cannot be proven statically",
		},
		{
			name:      "relative imports cannot escape the project",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "...domain", Line: 4, Literal: true},
			want:      "line 4 relative Python import escapes its project",
		},
		{
			name:      "third party imports do not need local graph edges",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "requests", Line: 5, Literal: true},
		},
		{
			name:      "local module imports require Ruff evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain.model", Line: 6, Literal: true},
			want:      "line 6 Ruff did not resolve local-looking Python import \"domain.model\"",
		},
		{
			name:      "local module evidence accepts the exact target",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain.model", Line: 7, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
		},
		{
			name:      "named import requires a resolvable local symbol",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "namespace", Names: []string{"missing"}, Line: 8, Literal: true},
			want:      "line 8 local-looking Python import \"namespace.missing\" cannot be resolved",
		},
		{
			name:      "named import accepts the target graph edge",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Names: []string{"model"}, Line: 9, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
		},
		{
			name:      "package initializer may reexport a named import",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Names: []string{"Model"}, Line: 10, Literal: true},
			dependencies: map[string]bool{
				"src/domain/__init__.py": true,
			},
		},
		{
			name:      "relative imports require their resolved graph edge",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: ".helper", Line: 11, Literal: true},
			dependencies: map[string]bool{
				"src/web/helper.py": true,
			},
		},
		{
			name:      "parent relative imports resolve from the package",
			source:    "src/nested/consumer.py",
			reference: pythonImportReference{Module: "..domain.model", Line: 12, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
		},
		{
			name:      "conflicting roots cannot prove a local import",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "shared", Line: 13, Literal: true},
			want:      "line 13 local-looking Python import \"shared\" resolves through conflicting source roots",
		},
		{
			name:      "direct package imports accept initializer evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Line: 14, Literal: true},
			dependencies: map[string]bool{
				"src/domain/__init__.py": true,
			},
		},
		{
			name:      "direct package imports reject sibling evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Line: 15, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
			want: "line 15 Ruff did not resolve local-looking Python import \"domain\"",
		},
		{
			name:      "namespace package imports do not need a graph edge",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "namespace", Line: 16, Literal: true},
		},
		{
			name:      "star imports require package initializer evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Names: []string{"*"}, Line: 17, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
			want: "line 17 Ruff did not resolve local-looking Python import \"domain\"",
		},
		{
			name:      "star imports accept package initializer evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Names: []string{"*"}, Line: 18, Literal: true},
			dependencies: map[string]bool{
				"src/domain/__init__.py": true,
			},
		},
		{
			name:      "source and stub paths share one module identity",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain.model", Line: 19, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.pyi": true,
			},
		},
		{
			name:      "ambiguous prefixes remain ambiguous for descendants",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "shared.child", Line: 20, Literal: true},
			want:      "line 20 local-looking Python import \"shared.child\" resolves through conflicting source roots",
		},
		{
			name:      "wrong graph targets cannot prove direct module imports",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain.model", Line: 21, Literal: true},
			dependencies: map[string]bool{
				"src/domain/__init__.py": true,
			},
			want: "line 21 Ruff did not resolve local-looking Python import \"domain.model\"",
		},
		{
			name:      "every named import needs evidence",
			source:    "src/web/consumer.py",
			reference: pythonImportReference{Module: "domain", Names: []string{"model", "missing"}, Line: 22, Literal: true},
			dependencies: map[string]bool{
				"src/domain/model.py": true,
			},
			want: "line 22 Ruff did not resolve local-looking Python import \"domain.missing\"",
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if message := pythonUnprovenImport(index, testCase.source, testCase.reference, testCase.dependencies); message != testCase.want {
				t.Fatalf("pythonUnprovenImport() = %q, want %q", message, testCase.want)
			}
		})
	}
}
