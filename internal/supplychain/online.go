package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const registryRequestTimeout = 2 * time.Minute

func checkGoOnlineWithCommands(ctx context.Context, repo repository.Repository, module repository.GoModule, commands []policy.Command, commandRunner runner.Runner) []policy.Finding {
	findings := []policy.Finding{}
	if len(commands) < 2 {
		return append(findings, policy.Finding{Check: "policy.supplyChain", Path: module.Manifest, Subject: "online command plan", Message: "Go online supply-chain command plan is incomplete"})
	}
	tidy := commands[0]
	if err := commandRunner.Run(ctx, repo.Root, tidy); err != nil {
		findings = append(findings, policy.Finding{Check: "supplyChain.goLock", Path: module.Manifest, Subject: "go mod tidy", Message: err.Error()})
	}
	findings = append(findings, goReleaseAgeWithCommand(ctx, repo, module, commands[1], commandRunner)...)
	if len(commands) == 2 {
		return append(findings, policy.Finding{Check: "policy.tool", Path: "repository", Subject: "govulncheck", Message: "pinned govulncheck is unavailable; run ./tools/install-policy-tools.sh"})
	}
	vulnerability := commands[2]
	if err := commandRunner.Run(ctx, repo.Root, vulnerability); err != nil {
		findings = append(findings, policy.Finding{Check: "supplyChain.goVulnerability", Path: module.Manifest, Subject: "govulncheck", Message: err.Error()})
	}
	return findings
}

func goOnlineCommands(repo repository.Repository, module repository.GoModule) []policy.Command {
	commands := []policy.Command{{
		Name: "go-mod-tidy-" + safeName(module.Root), Argv: []string{repo.GoTool("go"), "mod", "tidy", "-diff"}, Cwd: module.Root,
		Environment: repo.Config.SupplyChain.Environment, TimeoutSeconds: 900,
	}}
	commands = append(commands, policy.Command{
		Name: "go-release-age-" + safeName(module.Root), Argv: []string{repo.GoTool("go"), "list", "-m", "-json", "all"}, Cwd: module.Root,
		Environment: repo.Config.SupplyChain.Environment, TimeoutSeconds: 900,
	})
	govulncheck := repo.PolicyTool("govulncheck")
	if isExecutable(govulncheck) {
		commands = append(commands, policy.Command{
			Name: "govulncheck-" + safeName(module.Root), Argv: []string{govulncheck, "./..."}, Cwd: module.Root,
			Environment: repo.Config.SupplyChain.Environment, TimeoutSeconds: 1800,
		})
	}
	return commands
}

func goReleaseAgeWithCommand(ctx context.Context, repo repository.Repository, module repository.GoModule, command policy.Command, commandRunner runner.Runner) []policy.Finding {
	output, err := runCommandWithOutput(ctx, repo, command, commandRunner)
	if err != nil {
		return []policy.Finding{{Check: "supplyChain.releaseAge", Path: module.Manifest, Subject: "go list", Message: err.Error()}}
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Stdout))
	cutoff := time.Now().UTC().AddDate(0, 0, -repo.Config.SupplyChain.MinimumReleaseAgeDays)
	findings := []policy.Finding{}
	for {
		var item goListedModule
		if err := decoder.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return append(findings, policy.Finding{Check: "supplyChain.releaseAge", Path: module.Manifest, Subject: "go list", Message: err.Error()})
		}
		if finding := goReleaseAgeFinding(item, module.Manifest, cutoff, repo.Config.SupplyChain.MinimumReleaseAgeDays); finding != nil {
			findings = append(findings, *finding)
		}
	}
	return findings
}

func runCommandWithOutput(ctx context.Context, repo repository.Repository, command policy.Command, commandRunner runner.Runner) (runner.Output, error) {
	observed, ok := commandRunner.(runner.OutputRunner)
	if !ok {
		return runner.Output{}, fmt.Errorf("common command runner cannot capture output for %q", command.Name)
	}
	_, output, err := observed.RunWithOutput(ctx, repo.Root, command)
	return output, err
}

type goListedModule struct {
	Path    string
	Version string
	Time    time.Time
	Main    bool
	Replace *goListedModuleVersion
}

type goListedModuleVersion struct {
	Path    string
	Version string
	Time    time.Time
	Dir     string
}

func goReleaseAgeFinding(item goListedModule, manifest string, cutoff time.Time, minimumDays int) *policy.Finding {
	if item.Main || item.Version == "" || item.Replace != nil && item.Replace.Dir != "" {
		return nil
	}
	item = resolvedGoModule(item)
	if item.Time.IsZero() {
		return &policy.Finding{
			Check: "supplyChain.releaseAge", Path: manifest, Subject: item.Path + "@" + item.Version,
			Message: "Go module metadata did not include a release timestamp",
		}
	}
	if !item.Time.After(cutoff) {
		return nil
	}
	identity := policy.ReleaseAgeIdentity{
		Ecosystem: "go", Package: item.Path, Version: item.Version, Scope: manifest,
		Released: item.Time, Eligible: item.Time.AddDate(0, 0, minimumDays),
	}
	message := fmt.Sprintf("module was released %s and has not reached the %d-day hard minimum (eligible %s)", item.Time.Format(time.RFC3339), minimumDays, identity.Eligible.Format(time.RFC3339))
	return &policy.Finding{Check: "supplyChain.releaseAge", Path: manifest, Subject: policy.ReleaseAgeSubject(identity), Message: message, ReleaseAge: &identity}
}

func resolvedGoModule(item goListedModule) goListedModule {
	if item.Replace == nil || item.Replace.Version == "" {
		return item
	}
	item.Path = item.Replace.Path
	item.Version = item.Replace.Version
	item.Time = item.Replace.Time
	return item
}

func auditNodeWithCommand(ctx context.Context, repo repository.Repository, manifest nodeManifest, command policy.Command, commandRunner runner.Runner) []policy.Finding {
	scope, found := nodeLockPath(repo, manifest.Root, manifest.Manager)
	if !found {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: manifest.Path, Subject: "pnpm audit identity",
			Message: "native audit requires an exact supported lockfile",
		}}
	}
	output, runErr := runCommandWithOutput(ctx, repo, command, commandRunner)
	result, err := javascript.ParseAuditOutput(output.Stdout)
	if err != nil {
		message := err.Error()
		if detail := strings.TrimSpace(string(output.Stderr)); detail != "" {
			message += "; " + detail
		} else if runErr != nil {
			message += "; " + runErr.Error()
		}
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: scope, Subject: "pnpm audit", Message: message,
		}}
	}
	if runErr != nil {
		return []policy.Finding{{
			Check: "policy.securityScanner", Path: scope, Subject: "pnpm audit", Message: runErr.Error(),
		}}
	}
	return nodeAuditFindings(repo.Config.SupplyChain.AuditLevel, result, scope)
}

func nodeAuditFindings(auditLevel string, result javascript.AuditResult, path string) []policy.Finding {
	findings := []policy.Finding{}
	for _, entry := range result.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "policy.securityScanner", Path: entry.Path, Subject: "pnpm audit",
			Message: "the policy-owned native audit could not read a reported advisory: " + entry.Reason,
		})
	}
	for _, advisory := range result.Advisories {
		if !nodeAuditSeverityAtLeast(advisory.Severity, auditLevel) {
			continue
		}
		findings = append(findings, nodeAdvisoryFindings(advisory, path)...)
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Subject < findings[right].Subject
	})
	return uniqueFindings(findings)
}

func nodeAdvisoryFindings(advisory javascript.Advisory, path string) []policy.Finding {
	findings := []policy.Finding{}
	unusable := []string{}
	for _, version := range advisory.Versions {
		if !exactVersion.MatchString(version) {
			unusable = append(unusable, version)
			continue
		}
		findings = append(findings, nodeVulnerabilityFinding(path, advisory.ID, advisory.Aliases, advisory.Package, version, advisory.Severity, advisory.Title))
	}
	if len(findings) == 0 {
		return []policy.Finding{nodeAdvisoryCoverageFinding(path, advisory, advisory.Versions)}
	}
	if len(unusable) > 0 {
		findings = append(findings, nodeAdvisoryCoverageFinding(path, advisory, unusable))
	}
	return findings
}

func nodeAdvisoryCoverageFinding(path string, advisory javascript.Advisory, versions []string) policy.Finding {
	return policy.Finding{
		Check: "policy.securityScanner", Path: path, Subject: advisory.ID,
		Message: fmt.Sprintf("native audit reported %s against no exact release: %v", advisory.Package, versions),
	}
}

func nodeVulnerabilityFinding(path, advisory string, aliases []string, packageName, affected, severity, title string) policy.Finding {
	severity = policy.NormalizeVulnerabilitySeverity(severity)
	if title == "" {
		title = "no title reported"
	}
	identity := policy.VulnerabilityIdentity{
		Ecosystem: "pnpm", Advisory: advisory, Aliases: append([]string{}, aliases...), Package: packageName,
		AffectedVersion: affected, Scope: path, Severity: severity,
	}
	message := fmt.Sprintf("%s vulnerability in %s (%s)", strings.ToUpper(severity), packageName, title)
	return policy.Finding{
		Check: "supplyChain.nodeVulnerability", Path: path, Subject: policy.VulnerabilitySubject(identity),
		Message: message, Vulnerability: &identity,
	}
}

func nodeAuditSeverityAtLeast(severity, threshold string) bool {
	rank := map[string]int{"low": 1, "moderate": 2, "medium": 2, "high": 3, "critical": 4}
	observed, known := rank[strings.ToLower(severity)]
	if !known {
		return true
	}
	return observed >= rank[strings.ToLower(threshold)]
}

func registryClient() *http.Client {
	return &http.Client{
		Timeout:   registryRequestTimeout,
		Transport: githubTokenTransport{base: http.DefaultTransport, token: os.Getenv("GITHUB_TOKEN")},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("registry redirect must remain HTTPS")
			}
			if len(via) >= 5 {
				return errors.New("registry redirect limit exceeded")
			}
			return nil
		},
	}
}

type githubTokenTransport struct {
	base  http.RoundTripper
	token string
}

func (transport githubTokenTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "api.github.com" || transport.token == "" {
		return transport.base.RoundTrip(request)
	}
	if len(transport.token) > 4096 || strings.TrimSpace(transport.token) != transport.token {
		return nil, errors.New("GITHUB_TOKEN must contain one bounded token without surrounding whitespace")
	}
	authenticated := request.Clone(request.Context())
	authenticated.Header = request.Header.Clone()
	authenticated.Header.Set("Authorization", "Bearer "+transport.token)
	response, err := transport.base.RoundTrip(authenticated)
	if err != nil || response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		return response, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	public := request.Clone(request.Context())
	public.Header = request.Header.Clone()
	public.Header.Del("Authorization")
	return transport.base.RoundTrip(public)
}
