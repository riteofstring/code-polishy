package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

type packMigrationPlanCLIOptions struct {
	catalog, sha256 string
	selections      []string
}

func migratePacks(invocation invocation, arguments []string) int {
	if len(arguments) == 0 {
		return commandUsageError("pack", "pack migration requires plan, apply, or rollback")
	}
	switch arguments[0] {
	case "plan":
		return planPackMigration(invocation, arguments[1:])
	case "apply":
		return applyPackMigration(invocation, arguments[1:])
	case "rollback":
		return rollbackPackMigration(invocation, arguments[1:])
	default:
		return commandUsageError("pack", "pack migration requires plan, apply, or rollback")
	}
}

func planPackMigration(invocation invocation, arguments []string) int {
	options, err := parsePackMigrationPlanOptions(arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	result, err := engine.PlanPackMigration(context.Background(), engine.PackMigrationPlanOptions{
		RepositoryRoot: invocation.repoRoot, PolicyRoot: invocation.policyRoot, ConfigPath: invocation.configPath,
		CatalogPath: options.catalog, CatalogSHA256: options.sha256, References: options.selections,
	})
	if err != nil {
		return operationalError(err)
	}
	state := "READY"
	if !result.Plan.Ready {
		state = "BLOCKED"
	}
	fmt.Printf("PACK MIGRATION %s\n", state)
	fmt.Printf("PACKS: %s\n", packMigrationSelectionSummary(result.Plan.AfterPacks))
	fmt.Printf("INSTALLATION: %d installed, %d already present\n", result.Installed, result.AlreadyPresent)
	fmt.Printf("INVENTORY: %d files, %d native claims, %d selected-pack claims, %d custom checks\n", result.Plan.Inventory.Files, result.Plan.Inventory.NativeClaims, result.Plan.Inventory.PackClaims, len(result.Plan.Inventory.CustomClaims))
	for _, gap := range result.Plan.Gaps {
		fmt.Printf("GAP %s %s/%s %s: %s\n", gap.Path, gap.Capability, gap.Profile, gap.Language, gap.Message)
	}
	for _, diagnostic := range result.Plan.AddedDiagnostics {
		fmt.Printf("NEW %s %s %s: %s\n", strings.ToUpper(diagnostic.Severity), diagnostic.RuleID, diagnostic.Path, diagnostic.Message)
	}
	fmt.Printf("PLAN: %s\n", result.PlanPath)
	fmt.Println("ENGINE LOCK: unchanged")
	if result.Plan.Ready {
		fmt.Printf("NEXT: code-polishy pack migration apply --plan %s\n", result.PlanPath)
		return 0
	}
	return 1
}

func applyPackMigration(invocation invocation, arguments []string) int {
	planPath, err := parsePackMigrationPlanPath("pack migration apply", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	result, err := engine.ApplyPackMigration(context.Background(), engine.PackMigrationActionOptions{
		RepositoryRoot: invocation.repoRoot, PolicyRoot: invocation.policyRoot, ConfigPath: invocation.configPath, PlanPath: planPath,
	})
	if err != nil {
		return operationalError(err)
	}
	if result.Changed {
		fmt.Printf("PASS applied pack migration %s\n", result.Plan.MigrationID)
	} else {
		fmt.Printf("PASS pack migration %s was already applied\n", result.Plan.MigrationID)
	}
	fmt.Println("ENGINE LOCK: unchanged")
	fmt.Printf("ROLLBACK: code-polishy pack migration rollback --plan %s\n", planPath)
	return 0
}

func rollbackPackMigration(invocation invocation, arguments []string) int {
	planPath, err := parsePackMigrationPlanPath("pack migration rollback", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	result, err := engine.RollbackPackMigration(engine.PackMigrationActionOptions{
		RepositoryRoot: invocation.repoRoot, PolicyRoot: invocation.policyRoot, ConfigPath: invocation.configPath, PlanPath: planPath,
	})
	if err != nil {
		return operationalError(err)
	}
	if result.Changed {
		fmt.Printf("PASS rolled back pack migration %s\n", result.Plan.MigrationID)
	} else {
		fmt.Printf("PASS pack migration %s was already rolled back\n", result.Plan.MigrationID)
	}
	fmt.Println("ENGINE LOCK: unchanged")
	return 0
}

func parsePackMigrationPlanOptions(arguments []string) (packMigrationPlanCLIOptions, error) {
	result := packMigrationPlanCLIOptions{selections: []string{}}
	for len(arguments) > 0 {
		name, _, _ := strings.Cut(arguments[0], "=")
		if name != "--catalog" && name != "--sha256" && name != "--select" {
			return packMigrationPlanCLIOptions{}, fmt.Errorf("unknown pack migration plan option %q", arguments[0])
		}
		value, consumed, _, err := namedOptionValue(arguments, name)
		if err != nil {
			return packMigrationPlanCLIOptions{}, err
		}
		switch name {
		case "--catalog":
			if result.catalog != "" {
				return packMigrationPlanCLIOptions{}, errorsDuplicateOption("pack migration plan", name)
			}
			result.catalog = value
		case "--sha256":
			if result.sha256 != "" {
				return packMigrationPlanCLIOptions{}, errorsDuplicateOption("pack migration plan", name)
			}
			result.sha256 = value
		case "--select":
			result.selections = append(result.selections, value)
		}
		arguments = arguments[consumed:]
	}
	if result.catalog == "" || result.sha256 == "" || len(result.selections) == 0 {
		return packMigrationPlanCLIOptions{}, errors.New("pack migration plan requires --catalog PATH --sha256 DIGEST and one or more --select NAME@VERSION options")
	}
	return result, nil
}

func parsePackMigrationPlanPath(action string, arguments []string) (string, error) {
	values, positional, err := parsePackOptions(action, arguments, "--plan")
	if err != nil {
		return "", err
	}
	if len(positional) != 0 || values["--plan"] == "" {
		return "", fmt.Errorf("%s requires --plan PATH", action)
	}
	return values["--plan"], nil
}

func packMigrationSelectionSummary(selections []policy.PackSelection) string {
	values := make([]string, 0, len(selections))
	for _, selection := range selections {
		values = append(values, selection.Name+"@"+selection.Version+" "+selection.Digest)
	}
	return strings.Join(values, ", ")
}
