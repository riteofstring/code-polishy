package rustfacts

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustModuleGraphResolvesCyclicLocalGlobs(t *testing.T) {
	t.Parallel()
	graph := rustModuleFixture(t, map[string]string{
		"src/lib.rs":    "pub mod api;\npub mod domain;\npub use domain::*;\n",
		"src/api.rs":    "use crate::*;\npub fn value() { let _ = Value::new(); let _ = String::new(); assert_eq!(1, 1); }\n",
		"src/domain.rs": "use crate::*;\npub struct Value;\nimpl Value { pub fn new() -> Self { Self } }\n",
	})
	if len(graph.Findings) != 0 || len(graph.Sources) != 3 {
		t.Fatalf("valid circular globs lost source coverage: %+v", graph)
	}
	if !slices.ContainsFunc(graph.Imports, func(imported ModuleImport) bool {
		return imported.Source == "src/api.rs" && imported.Target == "src/domain.rs" && imported.Line == 2
	}) {
		t.Fatalf("glob lookup lost the defining source dependency: %+v", graph.Imports)
	}
}

func TestRustModuleGraphKeepsFailuresAroundCyclicGlobs(t *testing.T) {
	t.Parallel()
	for name, files := range map[string]map[string]string{
		"missing member": {
			"src/lib.rs":    "pub mod api;\npub mod domain;\npub use domain::*;\n",
			"src/api.rs":    "use crate::*;\npub fn value() { Missing::new(); }\n",
			"src/domain.rs": "use crate::*;\npub struct Value;\n",
		},
		"ambiguous member": {
			"src/lib.rs":    "pub mod api;\npub mod domain;\npub use api::*;\npub use domain::*;\npub fn value() { Value::new(); }\n",
			"src/api.rs":    "use crate::*;\npub struct Value;\n",
			"src/domain.rs": "use crate::*;\npub struct Value;\n",
		},
		"recursive aliases": {
			"src/lib.rs": "use crate::Second as First;\nuse crate::First as Second;\npub fn value() { First::new(); }\n",
		},
		"external glob": {
			"src/lib.rs": "use std::*;\npub fn value() { assert!(true); }\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			graph := rustModuleFixture(t, files)
			if len(graph.Findings) == 0 {
				t.Fatalf("unresolved imports acquired complete coverage: %+v", graph)
			}
		})
	}
}

func TestRustModuleGraphResolvesRootExternCrateAliases(t *testing.T) {
	t.Parallel()
	graph := rustModuleFixture(t, map[string]string{
		"src/lib.rs":    "extern crate self as cocoa;\nextern crate std as standard;\npub mod api;\npub mod domain;\n",
		"src/api.rs":    "use cocoa::domain::Value;\npub fn value() { let _ = Value::new(); let _ = standard::string::String::new(); }\n",
		"src/domain.rs": "pub struct Value;\nimpl Value { pub fn new() -> Self { Self } }\n",
	})
	if len(graph.Findings) != 0 {
		t.Fatalf("root extern-crate aliases were unavailable in a child: %+v", graph.Findings)
	}
	if !slices.ContainsFunc(graph.Imports, func(imported ModuleImport) bool {
		return imported.Source == "src/api.rs" && imported.Target == "src/domain.rs"
	}) {
		t.Fatalf("self-crate alias lost the defining source: %+v", graph.Imports)
	}
}

func TestRustModuleGraphKeepsExternCrateAliasesScoped(t *testing.T) {
	t.Parallel()
	for name, files := range map[string]map[string]string{
		"local shadow": {
			"src/lib.rs": "extern crate self as cocoa;\npub mod api;\npub struct Value;\n",
			"src/api.rs": "mod cocoa {}\npub fn value() { cocoa::Value::new(); }\n",
		},
		"child declaration": {
			"src/lib.rs":    "pub mod api;\npub mod domain;\n",
			"src/api.rs":    "pub fn value() { cocoa::domain::Value::new(); }\n",
			"src/domain.rs": "extern crate self as cocoa;\npub struct Value;\n",
		},
		"disabled prelude": {
			"src/lib.rs": "extern crate self as cocoa;\npub mod api;\npub struct Value;\n",
			"src/api.rs": "#![no_implicit_prelude]\npub fn value() { cocoa::Value::new(); }\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			graph := rustModuleFixture(t, files)
			if len(graph.Findings) == 0 {
				t.Fatalf("unavailable extern alias entered the prelude: %+v", graph)
			}
		})
	}
}

func rustModuleFixture(t *testing.T, sources map[string]string) ModuleGraph {
	t.Helper()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[workspace]\n")
	for path, content := range sources {
		write(t, repo.Root, path, content)
	}
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	discovery := Discover(t.Context(), repo, PlanDiscovery(repo, files, files), runner.OSRunner{})
	parsed := ReadSources(t.Context(), repo, PlanSources(repo, files), runner.OSRunner{})
	if len(discovery.Findings) != 0 || len(parsed.Findings) != 0 {
		t.Fatalf("graph fixture preparation failed: discovery=%+v syntax=%+v", discovery.Findings, parsed.Findings)
	}
	return ResolveModules(repo, discovery.Workspaces, parsed, files, files)
}
