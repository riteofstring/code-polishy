package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/riteofstring/code-polishy/internal/pack"
)

func handlePackMeta(invocation invocation) int {
	if len(invocation.arguments) == 0 {
		return commandUsageError("pack", "pack requires catalog, install, update, remove, verify, list, migration, conformance, or root")
	}
	action, arguments := invocation.arguments[0], invocation.arguments[1:]
	switch action {
	case "catalog":
		return showPackCatalog(arguments)
	case "install":
		return installPack(invocation, arguments)
	case "update":
		return updatePack(invocation, arguments)
	case "remove":
		return removePack(arguments)
	case "verify":
		return verifyPack(arguments, invocation.policyRoot)
	case "list":
		return listPacks(invocation, arguments)
	case "migration":
		return migratePacks(invocation, arguments)
	case "conformance":
		return runPackConformance(arguments)
	case "root":
		return printPackRoot(arguments)
	default:
		return commandUsageError("pack", "unknown pack action "+action)
	}
}

func verifyPack(arguments []string, policyRoot string) int {
	source, err := packSourceOption("pack verify", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	engineVersion, err := readPolicyVersion(policyRoot)
	if err != nil {
		return operationalError(err)
	}
	result, err := pack.VerifySource(context.Background(), source, policyRoot, engineVersion, pack.DefaultRunner())
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS verified pack %s %s with %d fixtures\n", result.Manifest.Name, result.Manifest.Version, result.Fixtures)
	return 0
}

func printPackRoot(arguments []string) int {
	if len(arguments) != 0 {
		return commandUsageError("pack", "pack root accepts no arguments")
	}
	root, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	fmt.Fprintln(os.Stdout, root)
	return 0
}

func runPackConformance(arguments []string) int {
	flags := flag.NewFlagSet("pack conformance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	ledger := flags.String("ledger", "", "language conformance ledger")
	reference := flags.String("reference", "", "reference Code Polishy executable")
	referencePolicyRoot := flags.String("reference-policy-root", "", "reference Code Polishy policy root")
	candidate := flags.String("candidate", "", "candidate Code Polishy executable")
	candidatePolicyRoot := flags.String("candidate-policy-root", "", "candidate Code Polishy policy root")
	candidatePacks := stringList{}
	flags.Var(&candidatePacks, "candidate-pack", "local pack source to install for the candidate lane")
	if err := flags.Parse(arguments); err != nil {
		return commandUsageError("pack", err.Error())
	}
	if flags.NArg() != 0 || *ledger == "" || *reference == "" || *candidate == "" {
		return commandUsageError("pack", "pack conformance requires --ledger PATH --reference PATH --candidate PATH and no positional arguments")
	}
	report, err := pack.RunConformance(context.Background(), pack.ConformanceOptions{
		LedgerPath:           *ledger,
		ReferenceExecutable:  *reference,
		ReferencePolicyRoot:  *referencePolicyRoot,
		CandidateExecutable:  *candidate,
		CandidatePolicyRoot:  *candidatePolicyRoot,
		CandidatePackSources: slices.Clone(candidatePacks),
	})
	if err != nil {
		return operationalError(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return operationalError(err)
	}
	if report.Summary.Status == "failed" {
		return 1
	}
	return 0
}

func packSourceOption(name string, arguments []string) (string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("source", "", "local pack source directory")
	if err := flags.Parse(arguments); err != nil {
		return "", err
	}
	if flags.NArg() != 0 || *source == "" {
		return "", fmt.Errorf("%s requires --source PATH and no positional arguments", name)
	}
	return *source, nil
}
