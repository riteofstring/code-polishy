package supplychain

import (
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestOSVAcceptsNoSourcesForValidatedDependencyFreePNPMLock(t *testing.T) {
	repo := supplyRepository(t)
	repo.PolicyRoot = installPackagesBundle(t, packagesResult(`{"path":".","manifest":"package.json","dependencies":[]}`, ""))
	repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	installNoSourcesOSV(t, repo.PolicyRoot)
	writeSupplyFile(t, repo.Root, "package.json", `{"packageManager":"pnpm@11.13.0"}`+"\n")
	writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	commands, err := osvCommands(repo)
	if err != nil {
		t.Fatal(err)
	}
	if findings := scanOSVWithCommands(t.Context(), repo, commands, runner.OSRunner{}); len(findings) != 0 {
		t.Fatalf("dependency-free scan findings = %+v", findings)
	}
}

func TestOSVNoSourcesRemainsFailClosedWithoutValidatedEmptyPNPMInventory(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		result   string
	}{
		{
			name:     "declared dependency",
			manifest: `{"packageManager":"pnpm@11.13.0","dependencies":{"example":"1.0.0"}}`,
			result:   packagesResult(`{"path":".","manifest":"package.json","dependencies":[]}`, ""),
		},
		{
			name:     "resolved package",
			manifest: `{"packageManager":"pnpm@11.13.0"}`,
			result: packagesResult(`{"path":".","manifest":"package.json","dependencies":[]}`,
				`{"name":"example","version":"1.0.0","source":"registry","licenseMetadata":"required"}`),
		},
		{
			name:     "unsupported lock",
			manifest: `{"packageManager":"pnpm@11.13.0"}`,
			result:   `{"lockfileVersion":"","importers":[],"packages":[],"unsupported":[{"path":"pnpm-lock.yaml","reason":"unsupported"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := supplyRepository(t)
			repo.PolicyRoot = installPackagesBundle(t, test.result)
			repo.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
			installNoSourcesOSV(t, repo.PolicyRoot)
			writeSupplyFile(t, repo.Root, "package.json", test.manifest+"\n")
			writeSupplyFile(t, repo.Root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
			commands, err := osvCommands(repo)
			if err != nil {
				t.Fatal(err)
			}
			findings := scanOSVWithCommands(t.Context(), repo, commands, runner.OSRunner{})
			if len(findings) != 1 || findings[0].Check != "policy.securityScanner" {
				t.Fatalf("no-source scan findings = %+v", findings)
			}
		})
	}
}

func installNoSourcesOSV(t *testing.T, root string) {
	t.Helper()
	writeBundleFile(t, filepath.Join(root, ".tools", "bin", "osv-scanner"), `#!/bin/sh
case " $* " in
  *" --allow-no-lockfiles "*) printf '{"results":null}\n' ;;
  *) printf 'No package sources found, --help for usage information.\n' >&2; exit 128 ;;
esac
`)
}
