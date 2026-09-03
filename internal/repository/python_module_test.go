package repository

import "testing"

func TestPythonModuleNameUsesProjectSourceRootAndPackageConventions(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		project     PythonProject
		source      string
		module      string
		packageName string
	}{
		"root module": {
			project: PythonProject{Root: "."}, source: "app/service.py", module: "app.service", packageName: "app",
		},
		"src module": {
			project: PythonProject{Root: "apps/api", SourceRoots: []string{"apps/api", "apps/api/src"}}, source: "apps/api/src/api/service.py", module: "api.service", packageName: "api",
		},
		"project-root module outside src": {
			project: PythonProject{Root: "apps/api", SourceRoots: []string{"apps/api", "apps/api/src"}}, source: "apps/api/scripts/task.py", module: "scripts.task", packageName: "scripts",
		},
		"package initializer": {
			project: PythonProject{Root: ".", SourceRoots: []string{".", "src"}}, source: "src/api/__init__.py", module: "api", packageName: "api",
		},
		"root package initializer": {
			project: PythonProject{Root: ".", SourceRoots: []string{".", "src"}}, source: "src/__init__.py", module: "", packageName: "",
		},
		"stub module": {
			project: PythonProject{Root: "."}, source: "api/protocol.pyi", module: "api.protocol", packageName: "api",
		},
		"outside project": {
			project: PythonProject{Root: "apps/api"}, source: "apps/other/main.py", module: "", packageName: "",
		},
		"non-python file": {
			project: PythonProject{Root: "."}, source: "api/service.txt", module: "", packageName: "",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			module, packageName := PythonModuleName(testCase.project, testCase.source)
			if module != testCase.module || packageName != testCase.packageName {
				t.Fatalf("PythonModuleName() = (%q, %q), want (%q, %q)", module, packageName, testCase.module, testCase.packageName)
			}
		})
	}
}
