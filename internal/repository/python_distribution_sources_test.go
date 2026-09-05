package repository

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonDistributionSourcesBindRecordedContentWithoutExecution(t *testing.T) {
	repo, project, dependency := distributionSourceFixture(t)
	first, err := repo.ReadPythonDistributionSources(project, dependency)
	if err != nil || len(first.Sources) != 1 || first.Identity == "" || !strings.HasSuffix(first.Sources[0].Path, "framework.py") {
		t.Fatalf("distribution snapshot = %+v, %v", first, err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, "executed")); !os.IsNotExist(err) {
		t.Fatalf("dependency code executed: %v", err)
	}
	writeFile(t, repo.Root, first.Root+"/unrelated-1.0.dist-info/METADATA", string([]byte{0xff}))
	unchanged, err := repo.ReadPythonDistributionSources(project, dependency)
	if err != nil || unchanged.Identity != first.Identity {
		t.Fatalf("unrelated distribution changed selected source custody: %+v, %v", unchanged, err)
	}
	changed := "class Contract:\n    def run(self):\n        return 2\n"
	writeFile(t, repo.Root, first.Sources[0].Path, changed)
	if _, err := repo.ReadPythonDistributionSources(project, dependency); err == nil {
		t.Fatal("modified source survived its recorded digest")
	}
	writeDistributionRecord(t, repo, first.Root, changed)
	second, err := repo.ReadPythonDistributionSources(project, dependency)
	if err != nil || second.Identity == first.Identity {
		t.Fatalf("current recorded content did not change identity: %+v, %v", second, err)
	}
}

func TestPythonDistributionSourcesRejectInvalidCustody(t *testing.T) {
	for name, mutate := range map[string]func(Repository, *PythonProject, *PythonPluginDependency){
		"absent environment": func(_ Repository, p *PythonProject, _ *PythonPluginDependency) { p.Venv = "" },
		"wrong version":      func(_ Repository, _ *PythonProject, d *PythonPluginDependency) { d.Version = "2.0" },
		"wrong distribution": func(_ Repository, _ *PythonProject, d *PythonPluginDependency) { d.Distribution = "other" },
		"duplicate record": func(r Repository, _ *PythonProject, _ *PythonPluginDependency) {
			name := filepath.Join(r.Root, ".venv/lib/python3.12/site-packages/framework-1.0.dist-info/RECORD")
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, append(data, data...), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"escaping source": func(r Repository, _ *PythonProject, _ *PythonPluginDependency) {
			writeFile(t, r.Root, ".venv/lib/python3.12/site-packages/framework-1.0.dist-info/RECORD", "../secret.py,sha256=invalid,1\n")
		},
		"source symlink": func(r Repository, _ *PythonProject, _ *PythonPluginDependency) {
			name := filepath.Join(r.Root, ".venv/lib/python3.12/site-packages/framework.py")
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "framework.py")
			if err := os.WriteFile(outside, []byte("secret = 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, name); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, project, dependency := distributionSourceFixture(t)
			mutate(repo, &project, &dependency)
			if _, err := repo.ReadPythonDistributionSources(project, dependency); err == nil {
				t.Fatal("invalid installed distribution supplied source evidence")
			}
		})
	}
}

func TestPythonDistributionSourcesEnforceSourceByteBoundary(t *testing.T) {
	repo, project, dependency := distributionSourceFixture(t)
	root := ".venv/lib/python3.12/site-packages"
	source := strings.Repeat("x", (1<<20)+1)
	writeFile(t, repo.Root, root+"/framework.py", source)
	writeDistributionRecord(t, repo, root, source)
	if _, err := repo.ReadPythonDistributionSources(project, dependency); err == nil || !strings.Contains(err.Error(), "exceeds 1048576 bytes") {
		t.Fatalf("recorded oversized source escaped its byte boundary: %v", err)
	}
}

func TestPythonDistributionSourcesReadWindowsEnvironmentLayout(t *testing.T) {
	repo, project, dependency := distributionSourceFixture(t)
	destination := filepath.Join(repo.Root, ".venv", "Lib", "site-packages")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo.Root, ".venv", "lib", "python3.12", "site-packages"), destination); err != nil {
		t.Fatal(err)
	}
	result, err := repo.ReadPythonDistributionSources(project, dependency)
	if err != nil || result.Root != ".venv/Lib/site-packages" || len(result.Sources) != 1 {
		t.Fatalf("Windows installation was not captured: %+v, %v", result, err)
	}
}

func distributionSourceFixture(t *testing.T) (Repository, PythonProject, PythonPluginDependency) {
	t.Helper()
	repo := Repository{Root: t.TempDir()}
	root := ".venv/lib/python3.12/site-packages"
	source := fmt.Sprintf("open(%q, 'w').write('executed')\nclass Contract:\n    def run(self):\n        return 1\n", filepath.Join(repo.Root, "executed"))
	writeFile(t, repo.Root, root+"/framework.py", source)
	writeFile(t, repo.Root, root+"/framework-1.0.dist-info/METADATA", "Metadata-Version: 2.4\nName: framework\nVersion: 1.0\n\n")
	writeDistributionRecord(t, repo, root, source)
	return repo, PythonProject{Manifest: "pyproject.toml", Root: ".", Venv: ".venv"}, PythonPluginDependency{Distribution: "framework", Version: "1.0", Kind: "registry", Source: "https://packages.example.test/simple"}
}

func writeDistributionRecord(t *testing.T, repo Repository, root, source string) {
	t.Helper()
	metadata := "Metadata-Version: 2.4\nName: framework\nVersion: 1.0\n\n"
	record := ""
	for _, input := range [][2]string{{"framework.py", source}, {"framework-1.0.dist-info/METADATA", metadata}} {
		digest := sha256.Sum256([]byte(input[1]))
		record += fmt.Sprintf("%s,sha256=%s,%d\n", input[0], base64.RawURLEncoding.EncodeToString(digest[:]), len(input[1]))
	}
	writeFile(t, repo.Root, root+"/framework-1.0.dist-info/RECORD", record+"framework-1.0.dist-info/RECORD,,\n")
}
