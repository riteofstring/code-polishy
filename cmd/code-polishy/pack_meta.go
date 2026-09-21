package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/pack"
)

func handlePackMeta(invocation invocation) int {
	if len(invocation.arguments) == 0 {
		return commandUsageError("pack", "pack requires install, verify, conformance, or root")
	}
	action, arguments := invocation.arguments[0], invocation.arguments[1:]
	switch action {
	case "install":
		return installPack(arguments)
	case "verify":
		return verifyPack(arguments, invocation.policyRoot)
	case "conformance":
		return runPackConformance(arguments)
	case "root":
		return printPackRoot(arguments)
	default:
		return commandUsageError("pack", "unknown pack action "+action)
	}
}

func installPack(arguments []string) int {
	source, err := packSourceOption("pack install", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	identity, _, err := pack.Install(source, dataRoot)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS installed pack %s %s %s\n", identity.Name, identity.Version, identity.Digest)
	return 0
}

func verifyPack(arguments []string, policyRoot string) int {
	source, err := packSourceOption("pack verify", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	result, err := pack.VerifySource(context.Background(), source, policyRoot, pack.DefaultRunner())
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
	candidate := flags.String("candidate", "", "candidate Code Polishy executable")
	if err := flags.Parse(arguments); err != nil {
		return commandUsageError("pack", err.Error())
	}
	if flags.NArg() != 0 || *ledger == "" || *reference == "" || *candidate == "" {
		return commandUsageError("pack", "pack conformance requires --ledger PATH --reference PATH --candidate PATH and no positional arguments")
	}
	report, err := pack.RunConformance(context.Background(), pack.ConformanceOptions{
		LedgerPath:          *ledger,
		ReferenceExecutable: *reference,
		CandidateExecutable: *candidate,
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
