package quality

import (
	"slices"
	"testing"
)

func TestPythonVultureBuiltInFrameworkContracts(t *testing.T) {
	cases := []struct {
		name, source string
		kept, dead   []string
	}{
		{"pytest", "import pytest\npytestmark = [pytest.mark.integration]\n@pytest.fixture(autouse=True)\ndef prepare_environment():\n    return 1\n@pytest.fixture\ndef unused_fixture():\n    return 2\ndef unused_helper():\n    return 3\n", []string{"pytestmark", "prepare_environment"}, []string{"unused_fixture", "unused_helper"}},
		{"sqlite", "import sqlite3\ndef configure():\n    connection = sqlite3.connect(':memory:')\n    connection.row_factory = sqlite3.Row\n    connection.unused_setting = True\n    return connection\n", []string{"row_factory"}, []string{"unused_setting"}},
		{"hypothesis", "from hypothesis.stateful import RuleBasedStateMachine\nclass ExampleMachine(RuleBasedStateMachine):\n    def teardown(self):\n        print('cleanup')\n    def unused_hook(self):\n        return 1\n", nil, []string{"teardown", "unused_hook"}},
		{"unrelated", "class Plain:\n    def teardown(self):\n        return 1\ndef configure(value):\n    value.row_factory = str\npytestmark = 1\n", nil, []string{"teardown", "row_factory", "pytestmark"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, response, _ := runTypedDictVulture(t, map[string]string{"src/example.py": test.source})
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 {
				t.Fatalf("analysis failed: %+v", response)
			}
			for _, name := range test.kept {
				if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("consumed %s reported dead: %+v", name, response.Diagnostics)
				}
			}
			for _, name := range test.dead {
				if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("unused %s hidden: %+v", name, response.Diagnostics)
				}
			}
		})
	}
}

func TestPythonVultureFrameworkAliasesAndReceiverBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		sources    map[string]string
		kept, dead []string
	}{
		{"reexports", map[string]string{
			"src/contracts.py": "from pytest import fixture as automatic\nfrom hypothesis.stateful import RuleBasedStateMachine as Machine\nclass BaseMachine(Machine):\n    pass\n",
			"src/example.py":   "from contracts import automatic, BaseMachine\nfrom pytest import mark as markers\npytestmark = markers.integration\n@automatic(autouse=True)\ndef initialize_case():\n    return 1\nclass MachineCase(BaseMachine):\n    def teardown(self):\n        print('cleanup')\n    def unused_hook(self):\n        return 1\n",
		}, []string{"pytestmark", "initialize_case"}, []string{"teardown", "unused_hook"}},
		{"connection aliases", map[string]string{"src/example.py": "from sqlite3 import connect as open_database, Connection\ndef configure():\n    connection = open_database(':memory:', factory=Connection)\n    alias = connection\n    alias.row_factory = str\n    alias.unused_setting = True\n    return alias\n"}, []string{"row_factory"}, []string{"unused_setting"}},
		{"connection context", map[string]string{"src/example.py": "import sqlite3\ndef configure():\n    with sqlite3.connect(':memory:') as connection:\n        connection.row_factory = sqlite3.Row\n        connection.unused_setting = True\n"}, []string{"row_factory"}, []string{"unused_setting"}},
		{"typed connection", map[string]string{"src/example.py": "from sqlite3 import Connection\ndef configure(connection: Connection):\n    connection.row_factory = str\n    connection.unused_setting = True\n"}, []string{"row_factory"}, []string{"unused_setting"}},
		{"shadowed pytest", map[string]string{"src/example.py": "import pytest\npytest = object()\npytestmark = pytest.mark.integration\n@pytest.fixture(autouse=True)\ndef unused_fixture():\n    return 1\n"}, nil, []string{"pytestmark", "unused_fixture"}},
		{"local pytest", map[string]string{"src/pytest.py": "def fixture(**options):\n    return options\n", "src/example.py": "import pytest\n@pytest.fixture(autouse=True)\ndef unused_fixture():\n    return 1\n"}, nil, []string{"unused_fixture"}},
		{"rebound connection", map[string]string{"src/example.py": "import sqlite3\ndef configure():\n    connection = sqlite3.connect(':memory:')\n    connection = object()\n    connection.row_factory = str\n"}, nil, []string{"row_factory"}},
		{"custom connection factory", map[string]string{"src/example.py": "import sqlite3\ndef configure(factory):\n    connection = sqlite3.connect(':memory:', factory=factory)\n    connection.row_factory = str\n"}, nil, []string{"row_factory"}},
		{"same-line receivers", map[string]string{"src/example.py": "import sqlite3\ndef configure(other):\n    connection = sqlite3.connect(':memory:')\n    connection.row_factory = str; other.row_factory = str\n"}, nil, []string{"row_factory"}},
		{"local hypothesis", map[string]string{"src/hypothesis/stateful.py": "class RuleBasedStateMachine:\n    pass\n", "src/example.py": "from hypothesis.stateful import RuleBasedStateMachine\nclass Plain(RuleBasedStateMachine):\n    def teardown(self):\n        return 1\n"}, nil, []string{"teardown"}},

		{"decorated pytest hooks", map[string]string{"src/example.py": "import pytest\n@pytest.hookimpl\ndef pytest_report_header():\n    return 'header'\n@pytest.hookimpl(wrapper=True)\ndef pytest_report_teststatus(report):\n    outcome = yield\n    return report, outcome\n@pytest.hookimpl(tryfirst=True)\ndef pytest_sessionfinish(session):\n    return session\ndef unused_helper():\n    return 1\n"}, []string{"pytest_report_header", "pytest_report_teststatus", "pytest_sessionfinish"}, []string{"unused_helper"}},
		{"hookimpl aliases", map[string]string{
			"src/contracts.py": "from pytest import hookimpl as automatic\n",
			"src/example.py":   "import pytest as framework\nfrom contracts import automatic\n@automatic\ndef pytest_configure(config):\n    return config\n@framework.hookimpl(wrapper=True)\ndef pytest_runtest_call(item):\n    yield item\n",
		}, []string{"pytest_configure", "pytest_runtest_call"}, nil},
		{"plugin class hookimpl", map[string]string{"src/example.py": "import pytest\nclass Plugin:\n    @pytest.hookimpl\n    def pytest_configure(self, config):\n        return config\n    def unused_method(self):\n        return 1\nprint(Plugin)\n"}, []string{"pytest_configure"}, []string{"unused_method"}},
		{"undecorated root conftest hooks", map[string]string{"conftest.py": "def pytest_sessionfinish(session):\n    return session\ndef pytest_configure(config):\n    return config\ndef unused_helper():\n    return 1\n"}, []string{"pytest_sessionfinish", "pytest_configure"}, []string{"unused_helper"}},
		{"undecorated nested conftest hooks", map[string]string{"tests/unit/conftest.py": "def pytest_report_header():\n    return 'header'\n"}, []string{"pytest_report_header"}, nil},
		{"conftest hook names outside module scope", map[string]string{"conftest.py": "class Holder:\n    def pytest_method(self):\n        return 1\ndef outer():\n    def pytest_nested():\n        return 2\n    return 3\nprint(Holder, outer)\n"}, nil, []string{"pytest_method", "pytest_nested"}},
		{"hook name outside conftest", map[string]string{"src/example.py": "def pytest_sessionfinish(session):\n    return session\n"}, nil, []string{"pytest_sessionfinish"}},
		{"conftest suffix", map[string]string{"src/my_conftest.py": "def pytest_sessionfinish(session):\n    return session\n"}, nil, []string{"pytest_sessionfinish"}},
		{"shadowed hookimpl", map[string]string{"src/example.py": "import pytest\npytest = object()\n@pytest.hookimpl\ndef unused_hook():\n    return 1\n"}, nil, []string{"unused_hook"}},
		{"local hookimpl", map[string]string{"src/pytest.py": "def hookimpl(function):\n    return function\n", "src/example.py": "import pytest\n@pytest.hookimpl\ndef unused_hook():\n    return 1\n"}, nil, []string{"unused_hook"}},
		{"unrelated hookimpl", map[string]string{"src/example.py": "import pluggy\nhookimpl = pluggy.HookimplMarker('other')\n@hookimpl\ndef unused_hook():\n    return 1\n"}, nil, []string{"unused_hook"}},
		{"non-autouse fixture", map[string]string{"src/example.py": "import pytest\n@pytest.fixture(autouse=False)\ndef unused_fixture():\n    return 1\ndef unrelated():\n    pytestmark = pytest.mark.integration\n"}, nil, []string{"unused_fixture", "pytestmark"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, response, _ := runTypedDictVulture(t, test.sources)
			if response.Error != "" || response.FactsError != "" || len(response.Problems) != 0 {
				t.Fatalf("analysis failed: %+v", response)
			}
			for _, name := range test.kept {
				if slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("consumed %s reported dead: %+v", name, response.Diagnostics)
				}
			}
			for _, name := range test.dead {
				if !slices.ContainsFunc(response.Diagnostics, func(d pythonVultureDiagnostic) bool { return d.Name == name }) {
					t.Fatalf("unused %s hidden: %+v", name, response.Diagnostics)
				}
			}
		})
	}
}
