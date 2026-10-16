package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestWindowsSealedTemporaryDirectory(t *testing.T) {
	t.Parallel()
	result, output, err := (OSRunner{}).RunWithOutput(t.Context(), t.TempDir(), policy.Command{
		Name: "private-temporary-directory", Argv: []string{os.Args[0], "-test.run=^TestWindowsSealedTemporaryDirectoryHelper$"},
		SealedEnvironment: true, TimeoutSeconds: 30,
		EnvironmentOverrides: []string{"CODE_POLISHY_TEMP_PROBE=1"},
	})
	if err != nil || result.ExitStatus != 0 {
		t.Fatalf("sealed temporary directory: %+v, %v; %s%s", result, err, output.Stdout, output.Stderr)
	}
	if !filepath.IsAbs(string(output.Stdout)) {
		t.Fatalf("temporary-file probe returned no absolute path: %q", output.Stdout)
	}
	if _, err := os.Stat(string(output.Stdout)); !os.IsNotExist(err) {
		t.Fatalf("private temporary file survived command cleanup: %q, %v", output.Stdout, err)
	}
}

func TestWindowsSealedTemporaryDirectoryHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_TEMP_PROBE") != "1" {
		return
	}
	if filepath.Clean(os.TempDir()) != filepath.Clean(os.Getenv("TMPDIR")) {
		t.Fatalf("Windows temporary directory %q differs from private directory %q", os.TempDir(), os.Getenv("TMPDIR"))
	}
	file, err := os.CreateTemp("", "native-probe-")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(os.Stdout, file.Name())
	os.Exit(0)
}
