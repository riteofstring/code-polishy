package repository

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestConfiguredTestScopeExtendsBuiltInClassification(t *testing.T) {
	t.Parallel()
	repo := Repository{Config: policy.Config{Tests: policy.Testing{Paths: []string{"checks/**"}}}}
	if !repo.IsTest("checks/runtime_check.py") || !repo.IsTest("src/runtime_test.py") || repo.IsTest("src/runtime.py") {
		t.Fatal("configured and built-in test classification did not remain additive")
	}
}
