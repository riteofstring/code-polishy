package runner

import (
	"slices"
	"testing"
)

func TestApplyEnvironmentOverridesReplacesOnlyExactNames(t *testing.T) {
	t.Parallel()
	got, err := applyEnvironmentOverrides([]string{"PATH=/bin", "VALUE=old", "OTHER=kept"}, []string{"VALUE=new", "CODE_POLISHY_EXECUTION_ID=run-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PATH=/bin", "OTHER=kept", "VALUE=new", "CODE_POLISHY_EXECUTION_ID=run-1"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestApplyEnvironmentOverridesRejectsMalformedOrDuplicateNames(t *testing.T) {
	t.Parallel()
	for _, overrides := range [][]string{{"MISSING"}, {"1BAD=value"}, {"DUP=one", "DUP=two"}, {"VALUE=bad\x00value"}} {
		if _, err := applyEnvironmentOverrides(nil, overrides); err == nil {
			t.Fatalf("invalid overrides passed: %q", overrides)
		}
	}
}
