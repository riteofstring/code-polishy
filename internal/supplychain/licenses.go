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
	"github.com/riteofstring/code-polishy/internal/spdx"
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
	admitted, err := spdx.Admitted(expression, allowed)
	if err != nil {
		return fmt.Errorf("resolved package declares %q, which is not a readable SPDX expression: %s", expression, err)
	}
	if !admitted {
		return fmt.Errorf("resolved package declares %q, which supplyChain.allowedLicenses does not admit", expression)
	}
	return nil
}
