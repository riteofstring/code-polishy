package supplychain

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type GitLabInspection struct {
	ControlInputs []string
	Findings      []policy.Finding
}

func InspectGitLab(ctx context.Context, repo repository.Repository) GitLabInspection {
	roots := GitLabRootPaths(repo)
	if len(roots) == 0 {
		return GitLabInspection{}
	}
	governed, err := repo.RawFiles()
	if err != nil {
		return GitLabInspection{
			ControlInputs: roots,
			Findings: []policy.Finding{{
				Check: "policy.inventory", Path: "repository", Subject: "raw-files",
				Message: fmt.Sprintf("cannot establish governed GitLab include inputs: %v", err),
			}},
		}
	}
	reported, err := (javascript.Bundle{PolicyRoot: repo.PolicyRoot}).GitLab(ctx, repo.Root, roots, governed)
	if err != nil {
		return GitLabInspection{
			ControlInputs: roots,
			Findings: []policy.Finding{{
				Check: "policy.tool", Path: "repository", Subject: "gitlab",
				Message: fmt.Sprintf("sealed GitLab configuration analysis is unavailable: %v", err),
			}},
		}
	}
	findings := gitLabFindings(reported)
	return GitLabInspection{ControlInputs: reported.Controls, Findings: uniqueFindings(findings)}
}

func GitLabRootPaths(repo repository.Repository) []string {
	roots := []string{}
	for _, path := range []string{".gitlab-ci.yml", ".gitlab-ci.yaml"} {
		if _, err := os.Lstat(filepath.Join(repo.Root, path)); err == nil {
			roots = append(roots, path)
		}
	}
	return roots
}

func gitLabFindings(reported javascript.GitLabResult) []policy.Finding {
	findings := []policy.Finding{}
	for _, unsupported := range reported.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "supplyChain.gitLabCoverage", Path: unsupported.Path, Subject: "static-analysis",
			Message: "GitLab configuration cannot be statically verified: " + unsupported.Reason,
		})
	}
	for _, image := range reported.Images {
		if !gitLabImagePinned(image.Image) {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.gitLabImagePin", Path: image.Path, Subject: image.Image,
				Message: image.Scope + " must use a full sha256 image digest",
			})
		}
	}
	for _, include := range reported.Includes {
		if finding, exists := gitLabIncludeFinding(include); exists {
			findings = append(findings, finding)
		}
	}
	return findings
}

func gitLabImagePinned(image string) bool {
	if image == "" || strings.TrimSpace(image) != image || strings.Contains(image, "$") {
		return false
	}
	name, digest, found := strings.Cut(image, "@")
	return found && name != "" && fullDigest.MatchString(digest)
}

func gitLabIncludeFinding(include javascript.GitLabInclude) (policy.Finding, bool) {
	switch include.Kind {
	case "project":
		if fullCommit.MatchString(include.Ref) {
			return policy.Finding{}, false
		}
		return policy.Finding{
			Check: "supplyChain.gitLabIncludePin", Path: include.Path, Subject: include.Project,
			Message: "project include must use a full 40-character commit ref",
		}, true
	case "remote":
		if secureGitLabRemote(include.Remote) && supportedGitLabIntegrity(include.Integrity) {
			return policy.Finding{}, false
		}
		return policy.Finding{
			Check: "supplyChain.gitLabIncludePin", Path: include.Path, Subject: include.Remote,
			Message: "remote include must use HTTPS without credentials and a supported sha256 integrity value",
		}, true
	case "component":
		if immutableGitLabComponent(include.Component) {
			return policy.Finding{}, false
		}
		return policy.Finding{
			Check: "supplyChain.gitLabIncludePin", Path: include.Path, Subject: include.Component,
			Message: "component include must use an immutable full 40-character commit identity",
		}, true
	case "template":
		return policy.Finding{}, false
	case "local":
		return policy.Finding{}, false
	default:
		return policy.Finding{
			Check: "supplyChain.gitLabCoverage", Path: include.Path, Subject: "static-analysis",
			Message: "GitLab configuration reports an unsupported include kind",
		}, true
	}
}

func secureGitLabRemote(value string) bool {
	if value == "" || strings.Contains(value, "$") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func supportedGitLabIntegrity(value string) bool {
	if !strings.HasPrefix(value, "sha256-") {
		return false
	}
	digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "sha256-"))
	return err == nil && len(digest) == 32
}

func immutableGitLabComponent(value string) bool {
	if value == "" || strings.Contains(value, "$") || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	identity, commit, found := strings.Cut(value, "@")
	return found && identity != "" && !strings.Contains(commit, "@") && fullCommit.MatchString(commit)
}
