package runner

import (
	"os"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestSealedEnvironmentProvidesWritableTemporaryDirectories(t *testing.T) {
	t.Parallel()
	environment, cleanup, err := commandEnvironment(policy.Command{SealedEnvironment: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	values := map[string]string{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		path := values[key]
		if path == "" || path == os.TempDir() {
			t.Fatalf("%s does not select a private temporary directory: %q", key, path)
		}
		file, err := os.CreateTemp(path, "probe-")
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	cleanup()
	if _, err := os.Stat(values["TEMP"]); !os.IsNotExist(err) {
		t.Fatalf("temporary directory survived cleanup: %v", err)
	}
}
