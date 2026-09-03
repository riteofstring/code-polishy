package supplychain

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGitLabInspectionReturnsRecursiveControlsAndStaticFindings(t *testing.T) {
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, ".gitlab-ci.yml", "include:\n  - local: ci/common.yml\n")
	writeSupplyFile(t, repo.Root, "ci/common.yml", "image: registry.example/app\n")
	commit := "0123456789abcdef0123456789abcdef01234567"
	result := `{"controls":[".gitlab-ci.yml","ci/common.yml"],` +
		`"images":[{"path":"ci/common.yml","scope":"global:image","image":"registry.example/app"}],` +
		`"includes":[{"path":".gitlab-ci.yml","kind":"local","local":"ci/common.yml","project":"","file":"","ref":"","remote":"","integrity":"","component":"","template":""},` +
		`{"path":"ci/common.yml","kind":"project","local":"","project":"group/templates","file":"release.yml","ref":"` + commit + `","remote":"","integrity":"","component":"","template":""}],` +
		`"unsupported":[{"path":"ci/common.yml","reason":"conditional include"}]}`
	repo.PolicyRoot = installBundle(t, map[string]string{"gitlab": result})
	inspection := InspectGitLab(context.Background(), repo)
	if !slices.Equal(inspection.ControlInputs, []string{".gitlab-ci.yml", "ci/common.yml"}) {
		t.Fatalf("controls = %v", inspection.ControlInputs)
	}
	if len(inspection.Findings) != 2 || !supplyChecks(inspection.Findings)["supplyChain.gitLabImagePin"] || !supplyChecks(inspection.Findings)["supplyChain.gitLabCoverage"] {
		t.Fatalf("findings = %+v", inspection.Findings)
	}
}

func TestGitLabStaticPinsRequireImmutableInputs(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	integrity := "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	valid := javascript.GitLabResult{
		Images: []javascript.GitLabImage{{Path: ".gitlab-ci.yml", Scope: "global:image", Image: "registry.example/app@sha256:" + strings.Repeat("a", 64)}},
		Includes: []javascript.GitLabInclude{
			{Path: ".gitlab-ci.yml", Kind: "project", Project: "group/templates", File: "release.yml", Ref: commit},
			{Path: ".gitlab-ci.yml", Kind: "remote", Remote: "https://ci.example/templates/release.yml", Integrity: integrity},
			{Path: ".gitlab-ci.yml", Kind: "component", Component: "gitlab.example/group/project/component@" + commit},
			{Path: ".gitlab-ci.yml", Kind: "template", Template: "Jobs/SAST.gitlab-ci.yml"},
		},
	}
	if findings := gitLabFindings(valid); len(findings) != 0 {
		t.Fatalf("immutable GitLab inputs were rejected: %+v", findings)
	}
	for name, result := range map[string]javascript.GitLabResult{
		"tag image":                {Images: []javascript.GitLabImage{{Path: ".gitlab-ci.yml", Scope: "global:image", Image: "registry.example/app:stable"}}},
		"tag project ref":          {Includes: []javascript.GitLabInclude{{Path: ".gitlab-ci.yml", Kind: "project", Project: "group/templates", File: "release.yml", Ref: "main"}}},
		"remote without integrity": {Includes: []javascript.GitLabInclude{{Path: ".gitlab-ci.yml", Kind: "remote", Remote: "https://ci.example/release.yml"}}},
		"remote credentials":       {Includes: []javascript.GitLabInclude{{Path: ".gitlab-ci.yml", Kind: "remote", Remote: "https://token@ci.example/release.yml", Integrity: integrity}}},
		"component tag":            {Includes: []javascript.GitLabInclude{{Path: ".gitlab-ci.yml", Kind: "component", Component: "gitlab.example/group/project/component@1.2.3"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if findings := gitLabFindings(result); len(findings) != 1 || findings[0].Check != "supplyChain.gitLabImagePin" && findings[0].Check != "supplyChain.gitLabIncludePin" {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestGitLabConfigurationNeverProvesSecurityMonitoringCadence(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	repo.Config.Project.Capabilities = []string{"custom-dependencies"}
	writeSupplyFile(t, repo.Root, ".gitlab-ci.yml", "security:\n  script: code-polishy supply-chain\n")
	files := []string{".gitlab-ci.yml"}
	findings := securityMonitoringFindings(repo, files)
	if len(findings) != 1 || findings[0].Check != "policy.securityMonitoring" {
		t.Fatalf("GitLab YAML was treated as schedule evidence: %+v", findings)
	}
	repo.Config.Checks = []policy.Command{{Name: "gitlab-schedules", Provides: []string{"security-monitoring"}, RunOn: []string{"security"}}}
	if findings = securityMonitoringFindings(repo, files); len(findings) != 0 {
		t.Fatalf("explicit external monitoring provider was rejected: %+v", findings)
	}
}

func TestGitLabRootPathsIncludeBothSupportedNames(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	writeSupplyFile(t, repo.Root, ".gitlab-ci.yml", "stages: []\n")
	writeSupplyFile(t, repo.Root, ".gitlab-ci.yaml", "stages: []\n")
	if roots := GitLabRootPaths(repo); !slices.Equal(roots, []string{".gitlab-ci.yml", ".gitlab-ci.yaml"}) {
		t.Fatalf("roots = %v", roots)
	}
}
