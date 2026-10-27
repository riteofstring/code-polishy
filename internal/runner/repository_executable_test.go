package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestOSRunnerExecutesPinnedVirtualenvInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix virtualenv interpreter links")
	}
	for _, layout := range []string{"direct", "framework"} {
		t.Run(layout, func(t *testing.T) {
			root, environment := virtualenvExecutableFixture(t)
			if layout == "framework" {
				linkVirtualenvFramework(t, root, environment)
			}
			result, output, err := RunCaptured(t.Context(), OSRunner{}, root, policy.Command{
				Name: "python-build", Argv: []string{environment + "/bin/python", "-m", "build", "--no-isolation"},
				Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds,
			}, CaptureStructured)
			if err != nil || result.ExitStatus != 0 || !strings.HasSuffix(string(output.Stdout), "/project/.venv/bin/python|-m build --no-isolation") {
				t.Fatalf("virtualenv invocation = %+v, %q, %v", result, output.Stdout, err)
			}
		})
	}
}

func TestRepositoryExecutableRejectsDifferentInterpreterThroughHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix virtualenv interpreter links")
	}
	root, environment := virtualenvExecutableFixture(t)
	linkVirtualenvFramework(t, root, environment)
	other := filepath.Join(t.TempDir(), "python3.12")
	if err := os.WriteFile(other, []byte("#!/bin/sh\nprintf 'other interpreter'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, environment, "bin", "python")
	if err := os.Remove(candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, candidate); err != nil {
		t.Fatal(err)
	}
	if path, err := RepositoryExecutable(root, environment+"/bin/python"); err == nil {
		t.Fatalf("unrelated interpreter accepted through home: %s", path)
	}
}

func TestRepositoryExecutableRejectsUnboundExternalInterpreters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix virtualenv interpreter links")
	}
	for _, test := range []struct {
		name, path, value string
	}{
		{"mismatched pin", "project/.python-version", "3.13.1\n"},
		{"unbounded pin", "project/.python-version", "3.12\n"},
		{"absent pin", "project/.python-version", ""},
		{"missing environment", "project/.venv/pyvenv.cfg", ""},
		{"wrong home", "project/.venv/pyvenv.cfg", "version_info = 3.12.13\nhome = /unrelated\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, environment := virtualenvExecutableFixture(t)
			path := filepath.Join(root, test.path)
			if test.value == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(test.value), 0o600); err != nil {
				t.Fatal(err)
			}
			if path, err := RepositoryExecutable(root, environment+"/bin/python"); err == nil {
				t.Fatalf("unbound interpreter accepted: %s", path)
			}
		})
	}
	root, environment := virtualenvExecutableFixture(t)
	target, err := filepath.EvalSymlinks(filepath.Join(root, environment, "bin", "python"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "script")); err != nil {
		t.Fatal(err)
	}
	if _, err := RepositoryExecutable(root, "./script"); err == nil {
		t.Fatal("ordinary escaping script accepted")
	}
}

func virtualenvExecutableFixture(t *testing.T) (string, string) {
	t.Helper()
	root, external := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "project", ".venv", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		filepath.Join(root, "project", ".python-version"):     "3.12.13\n",
		filepath.Join(root, "project", ".venv", "pyvenv.cfg"): "home = " + external + "\nversion_info = 3.12.13\n",
		filepath.Join(external, "python3.12"):                 "#!/bin/sh\nprintf '%s|%s' \"$0\" \"$*\"\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(external, "python3.12"), filepath.Join(root, "project", ".venv", "bin", "python")); err != nil {
		t.Fatal(err)
	}
	return root, "project/.venv"
}

func linkVirtualenvFramework(t *testing.T, root, environment string) {
	t.Helper()
	interpreter, err := filepath.EvalSymlinks(filepath.Join(root, environment, "bin", "python"))
	if err != nil {
		t.Fatal(err)
	}
	framework := filepath.Join(filepath.Dir(interpreter), "Frameworks", "Python.framework", "Versions", "3.12", "bin", "python3.12")
	if err := os.MkdirAll(filepath.Dir(framework), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(interpreter, framework); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(framework, interpreter); err != nil {
		t.Fatal(err)
	}
}
