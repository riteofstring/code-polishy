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

const licenseFactsBudget = 10 * time.Minute

func pnpmLicenseFindings(ctx context.Context, repo repository.Repository, resolved javascript.PackageResult, directory, lock string) []policy.Finding {
	allowed := licenseAllowList(repo.Config.SupplyChain.AllowedLicenses)

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

func licenseAllowList(licenses []string) map[string]bool {
	allowed := map[string]bool{}
	for _, license := range licenses {
		allowed[strings.ToLower(license)] = true
	}
	return allowed
}

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

func licenseTokens(expression string) []string {
	spaced := strings.ReplaceAll(strings.ReplaceAll(expression, "(", " ( "), ")", " ) ")
	return strings.Fields(spaced)
}

type licenseReader struct {
	tokens   []string
	position int
	allowed  map[string]bool
}

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
