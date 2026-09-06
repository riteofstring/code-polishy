package quality

import (
	"slices"
	"testing"
)

func TestPythonVultureFrameworkCallbackContracts(t *testing.T) {
	cases := []struct {
		name, source string
		kept, dead   []string
	}{
		{"stateful registration", "from hypothesis.stateful import RuleBasedStateMachine, invariant\nclass Machine(RuleBasedStateMachine):\n    @invariant()\n    def consistent_state(self):\n        return True\n    def unused_hook(self):\n        return 1\n", nil, []string{"consistent_state", "unused_hook"}},
		{"buffered input", "import io\nclass Input(io.RawIOBase):\n    def readable(self):\n        return True\n    def readinto(self, buffer):\n        return 0\n    def unused_hook(self):\n        return 1\nreader = io.BufferedReader(Input())\n", []string{"readable", "readinto"}, []string{"unused_hook"}},
		{"HTTP dispatch", "from http.server import BaseHTTPRequestHandler\nclass Handler(BaseHTTPRequestHandler):\n    def do_GET(self):\n        pass\n    def do_POST(self):\n        pass\n    def log_message(self, message, *args):\n        pass\n    def unused_hook(self):\n        pass\n", []string{"do_GET", "do_POST", "log_message"}, []string{"unused_hook"}},
		{"unrelated callbacks", "class Plain:\n    def readable(self):\n        return True\n    def readinto(self, buffer):\n        return 0\n    def do_GET(self):\n        pass\n    def log_message(self, message):\n        pass\n", nil, []string{"readable", "readinto", "do_GET", "log_message"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFrameworkDiagnostics(t, map[string]string{"src/example.py": test.source}, test.kept, test.dead)
		})
	}
}

func TestPythonVultureSQLiteConstructionFlow(t *testing.T) {
	cases := []struct {
		name, source string
		lines        []int
	}{
		{"instance connection", "import sqlite3\nclass Store:\n    def __init__(self):\n        self.connection = sqlite3.connect(':memory:')\n        self.connection.row_factory = sqlite3.Row\n", nil},
		{"try construction", "import sqlite3\ndef configure():\n    connection = None\n    try:\n        connection = sqlite3.connect(':memory:')\n        connection.row_factory = sqlite3.Row\n    finally:\n        if connection is not None:\n            connection.close()\n", nil},
		{"try without initialization", "import sqlite3\ndef configure():\n    try:\n        connection = sqlite3.connect(':memory:')\n        connection.row_factory = sqlite3.Row\n    except OSError:\n        pass\n", nil},
		{"both branches construct", "import sqlite3\ndef configure(flag):\n    if flag:\n        connection = sqlite3.connect(':memory:')\n    else:\n        connection = sqlite3.connect(':memory:')\n    connection.row_factory = sqlite3.Row\n", nil},
		{"instance mutation call", "import sqlite3\nclass Store:\n    def configure(self, mutate):\n        self.connection = sqlite3.connect(':memory:')\n        mutate(self)\n        self.connection.row_factory = str\n", []int{6}},

		{"rebound instance", "import sqlite3\nclass Store:\n    def configure(self, other):\n        self.connection = sqlite3.connect(':memory:')\n        self.connection.row_factory = sqlite3.Row\n        self.connection = other\n        self.connection.row_factory = str\n", []int{7}},
		{"branch merge", "import sqlite3\ndef configure(condition):\n    connection = None\n    if condition:\n        connection = sqlite3.connect(':memory:')\n        connection.row_factory = sqlite3.Row\n    connection.row_factory = str\n", []int{7}},
		{"exception path", "import sqlite3\ndef configure():\n    connection = None\n    try:\n        connection = sqlite3.connect(':memory:')\n        connection.row_factory = sqlite3.Row\n    except OSError:\n        connection.row_factory = str\n", []int{8}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, response, _ := runTypedDictVulture(t, map[string]string{"src/example.py": test.source})
			if response.Error != "" || response.FactsError != "" {
				t.Fatalf("analysis failed: %+v", response)
			}
			var lines []int
			for _, diagnostic := range response.Diagnostics {
				if diagnostic.Name == "row_factory" {
					lines = append(lines, diagnostic.Line)
				}
			}
			if !slices.Equal(lines, test.lines) {
				t.Fatalf("row_factory findings at %v, want %v: %+v", lines, test.lines, response.Diagnostics)
			}
		})
	}
}

func assertFrameworkDiagnostics(t *testing.T, sources map[string]string, kept, dead []string) {
	t.Helper()
	_, _, response, _ := runTypedDictVulture(t, sources)
	if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 {
		t.Fatalf("analysis failed: %+v", response)
	}
	for _, name := range kept {
		if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("consumed %s reported dead: %+v", name, response.Diagnostics)
		}
	}
	for _, name := range dead {
		if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
			t.Fatalf("unused %s hidden: %+v", name, response.Diagnostics)
		}
	}
}

func TestPythonVultureFrameworkAncestryAndDecoratorIdentity(t *testing.T) {
	sources := map[string]string{
		"src/bases.py":     "from io import RawIOBase as Raw\nfrom http.server import BaseHTTPRequestHandler as HTTP\nfrom hypothesis.stateful import RuleBasedStateMachine as Stateful\nclass InputBase(Raw):\n    pass\nclass HandlerBase(HTTP):\n    pass\nclass MachineBase(Stateful):\n    pass\n",
		"src/consumers.py": "from bases import InputBase, HandlerBase, MachineBase\nfrom hypothesis.stateful import invariant as checked, rule, initialize\nclass Input(InputBase):\n    def readable(self):\n        return True\n    def readinto(self, buffer):\n        return 0\nclass Handler(HandlerBase):\n    def do_CUSTOM(self):\n        pass\n    def log_message(self, message, *args):\n        pass\nclass Machine(MachineBase):\n    @checked()\n    def consistent_state(self):\n        return True\n    @rule()\n    def transition(self):\n        pass\n    @initialize()\n    def initial_state(self):\n        pass\n    def unused_hook(self):\n        pass\n",
	}
	assertFrameworkDiagnostics(t, sources, []string{"readable", "readinto", "buffer", "do_CUSTOM", "log_message", "message", "args"}, []string{"consistent_state", "transition", "initial_state", "unused_hook"})
	for name, source := range map[string]string{
		"unrelated decorator": "from hypothesis.stateful import invariant\nclass Plain:\n    @invariant()\n    def unused_hook(self):\n        pass\n",
		"shadowed decorator":  "from hypothesis.stateful import RuleBasedStateMachine, invariant\ninvariant = lambda: (lambda value: value)\nclass Machine(RuleBasedStateMachine):\n    @invariant()\n    def unused_hook(self):\n        pass\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertFrameworkDiagnostics(t, map[string]string{"src/example.py": source}, nil, []string{"unused_hook"})
		})
	}
}

func TestPythonVultureSQLiteUncertainFlowKeepsFindings(t *testing.T) {
	bodies := map[string]string{
		"conditional construction":   "    connection = None\n    if flag:\n        connection = sqlite3.connect(':memory:')\n    connection.row_factory = str\n",
		"loop may not execute":       "    connection = None\n    for item in flag:\n        connection = sqlite3.connect(':memory:')\n    connection.row_factory = str\n",
		"later loop iteration":       "    connection = sqlite3.connect(':memory:')\n    for item in flag:\n        connection.row_factory = str\n        connection = None\n",
		"short circuit":              "    connection = None\n    flag and (connection := sqlite3.connect(':memory:'))\n    connection.row_factory = str\n",
		"suppressed exception":       "    connection = None\n    with flag:\n        connection = sqlite3.connect(':memory:')\n    connection.row_factory = str\n",
		"delete receiver":            "    connection = sqlite3.connect(':memory:')\n    del connection\n    connection.row_factory = str\n",
		"closure rebind":             "    connection = sqlite3.connect(':memory:')\n    def replace():\n        nonlocal connection\n        connection = None\n    replace()\n    connection.row_factory = str\n",
		"finally may follow failure": "    connection = None\n    try:\n        connection = sqlite3.connect(':memory:')\n    finally:\n        connection.row_factory = str\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			assertFrameworkDiagnostics(t, map[string]string{"src/example.py": "import sqlite3\ndef configure(flag):\n" + body}, nil, []string{"row_factory"})
		})
	}
}

func TestPythonVultureUnresolvedFrameworkIdentityDoesNotHideWrites(t *testing.T) {
	assertFrameworkDiagnostics(t, map[string]string{
		"src/bridge.py":    "from contracts import Connection\n",
		"src/contracts.py": "from bridge import Connection\n",
		"src/example.py":   "from bridge import Connection\ndef configure(connection: Connection):\n    connection.row_factory = str\n",
	}, nil, []string{"row_factory"})
}
