package supplychain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

// How long the sealed bundle has to read one pnpm project's installed license
// metadata. It opens one small manifest per stored release and resolves
// nothing.
const licenseFactsBudget = 10 * time.Minute

// pnpmLicenseFindings decides what every release one pnpm project installed is
// licensed under.
//
// The lockfile says what the target installs; only each installed manifest says
// what it may be used under, so the sealed bundle reads that metadata and this
// decides it. The policy is the target's own configured list: a repository that
// declares no allowed licenses declares no license policy, and none is invented
// for it.
func pnpmLicenseFindings(ctx context.Context, repo repository.Repository, resolved javascript.PackageResult, directory, lock string) []policy.Finding {
	allowed := licenseAllowList(repo.Config.SupplyChain.AllowedLicenses)
	// A lock the reader could not read resolves nothing, so which releases this
	// lane governs is unknown. That is already reported as missing lock
	// coverage; reporting it again as missing license coverage would name the
	// same root cause twice.
	if len(allowed) == 0 || resolved.LockfileVersion == "" {
		return nil
	}
	factsContext, cancel := context.WithTimeout(ctx, licenseFactsBudget)
	defer cancel()
	installed, err := javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Licenses(factsContext, repo.Root, directory)
	if err != nil {
		return []policy.Finding{{
			Check: "policy.tool", Path: lock, Subject: "javascript-bundle", Message: err.Error(),
		}}
	}
	findings := []policy.Finding{}
	for _, entry := range installed.Unsupported {
		findings = append(findings, policy.Finding{
			Check: "supplyChain.licenseCoverage", Path: entry.Path, Subject: "pnpm",
			Message: "the policy-owned license reader could not read installed metadata: " + entry.Reason,
		})
	}
	return append(findings, resolvedLicenseFindings(resolved, installed, allowed, lock)...)
}

// resolvedLicenseFindings decides one finding per release the lock resolved.
// The graph decides which releases are in scope, not the installed tree: a
// stored package no lockfile resolves is not something the target installs. A
// resolved release with metadata is always evaluated; when metadata is absent,
// the sealed host fact decides whether its absence is expected or blocking.
func resolvedLicenseFindings(resolved javascript.PackageResult, installed javascript.LicenseResult, allowed map[string]bool, lock string) []policy.Finding {
	declared := map[string]string{}
	for _, item := range installed.Packages {
		declared[item.Name+"@"+item.Version] = item.License
	}
	findings := []policy.Finding{}
	reported := map[string]bool{}
	for _, item := range resolved.Packages {
		subject := item.Name + "@" + item.Version
		if item.Source != "registry" || !exactVersion.MatchString(item.Version) || reported[subject] {
			continue
		}
		reported[subject] = true
		license, found := declared[subject]
		if !found {
			if item.LicenseMetadata == javascript.LicenseMetadataPlatformExcluded {
				continue
			}
			message := "the installed dependency tree declares no license metadata for this resolved release"
			if item.LicenseMetadata == javascript.LicenseMetadataUnknown {
				message += " because its lockfile platform metadata is undecidable"
			}
			findings = append(findings, policy.Finding{
				Check: "supplyChain.licenseCoverage", Path: lock, Subject: subject,
				Message: message,
			})
			continue
		}
		if err := licenseAdmitted(license, allowed); err != nil {
			findings = append(findings, policy.Finding{
				Check: "supplyChain.dependencyLicense", Path: lock, Subject: subject, Message: err.Error(),
			})
		}
	}
	return findings
}

// licenseAllowList is the configured policy as the expression reader compares
// against it. SPDX matches identifiers case insensitively, so the comparison
// does too rather than refusing a package for writing "mit".
func licenseAllowList(licenses []string) map[string]bool {
	allowed := map[string]bool{}
	for _, license := range licenses {
		allowed[strings.ToLower(license)] = true
	}
	return allowed
}

// licenseAdmitted decides whether one declared SPDX expression is satisfied by
// the configured policy, and explains exactly why it is not. An expression this
// reader cannot parse is refused rather than admitted on a guess: a license
// nobody can name is not one the target reviewed.
func licenseAdmitted(expression string, allowed map[string]bool) error {
	if strings.TrimSpace(expression) == "" {
		return errors.New("resolved package declares no license, which supplyChain.allowedLicenses does not admit")
	}
	reader := &licenseReader{tokens: licenseTokens(expression), allowed: allowed}
	admitted, err := reader.disjunction()
	if err == nil && reader.position != len(reader.tokens) {
		err = fmt.Errorf("unexpected %q", reader.tokens[reader.position])
	}
	if err != nil {
		return fmt.Errorf("resolved package declares %q, which is not a readable SPDX expression: %s", expression, err)
	}
	if !admitted {
		return fmt.Errorf("resolved package declares %q, which supplyChain.allowedLicenses does not admit", expression)
	}
	return nil
}

// licenseTokens splits one expression into identifiers, operators, and the
// parentheses that group them. SPDX separates every token by whitespace except
// around parentheses, so only those need separating.
func licenseTokens(expression string) []string {
	spaced := strings.ReplaceAll(strings.ReplaceAll(expression, "(", " ( "), ")", " ) ")
	return strings.Fields(spaced)
}

// licenseReader parses one declared expression against the allow list. It
// evaluates while it parses because a policy decision is all Code Polishy wants
// from an expression: no tree of it is kept, compared, or reported.
type licenseReader struct {
	tokens   []string
	position int
	allowed  map[string]bool
}

// disjunction admits an expression when either side of an OR is admitted, which
// is what a dual-licensed package offers: the target may use it under whichever
// license its policy allows.
func (reader *licenseReader) disjunction() (bool, error) {
	admitted, err := reader.conjunction()
	if err != nil {
		return false, err
	}
	for reader.accept("OR") {
		alternative, err := reader.conjunction()
		if err != nil {
			return false, err
		}
		admitted = admitted || alternative
	}
	return admitted, nil
}

// conjunction admits an expression only when both sides of an AND are admitted:
// a package offered under two licenses at once binds the target to both.
func (reader *licenseReader) conjunction() (bool, error) {
	admitted, err := reader.term()
	if err != nil {
		return false, err
	}
	for reader.accept("AND") {
		additional, err := reader.term()
		if err != nil {
			return false, err
		}
		admitted = admitted && additional
	}
	return admitted, nil
}

// term is one license, one license qualified by an exception, or a
// parenthesized expression. A WITH exception changes what the license permits,
// so the qualified pair has to be allowed as written rather than the license
// alone.
func (reader *licenseReader) term() (bool, error) {
	if reader.accept("(") {
		admitted, err := reader.disjunction()
		if err != nil {
			return false, err
		}
		if !reader.accept(")") {
			return false, errors.New("a group is never closed")
		}
		return admitted, nil
	}
	license, err := reader.identifier()
	if err != nil {
		return false, err
	}
	if reader.accept("WITH") {
		exception, err := reader.identifier()
		if err != nil {
			return false, err
		}
		license += " WITH " + exception
	}
	return reader.allowed[strings.ToLower(license)], nil
}

func (reader *licenseReader) identifier() (string, error) {
	if reader.position == len(reader.tokens) {
		return "", errors.New("a license is missing")
	}
	token := reader.tokens[reader.position]
	if !policy.IsLicenseIdentifier(token) || licenseOperator(token) {
		return "", fmt.Errorf("%q is not a license identifier", token)
	}
	reader.position++
	return token, nil
}

func (reader *licenseReader) accept(token string) bool {
	if reader.position == len(reader.tokens) || reader.tokens[reader.position] != token {
		return false
	}
	reader.position++
	return true
}

func licenseOperator(token string) bool {
	return token == "AND" || token == "OR" || token == "WITH"
}
