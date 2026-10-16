package runner

import (
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestSelectedEnvironmentRetainsNativeNameSemantics(t *testing.T) {
	t.Setenv("Code_Polishy_Selected", "sdk-root")
	t.Setenv("CODE_POLISHY_EMPTY", "")
	t.Setenv("CODE_POLISHY_UNSELECTED", "not-inherited")
	environment := explicitlySelectedEnvironment([]string{"CODE_POLISHY_SELECTED", "CODE_POLISHY_EMPTY"})
	if slices.Contains(environment, "CODE_POLISHY_SELECTED=sdk-root") != (runtime.GOOS == "windows") {
		t.Fatalf("incorrect native environment-name lookup: %v", environment)
	}
	if !slices.Contains(environment, "CODE_POLISHY_EMPTY=") || slices.Contains(environment, "CODE_POLISHY_UNSELECTED=not-inherited") {
		t.Fatalf("selected environment lost an empty value or inherited an unselected value: %v", environment)
	}
}

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
