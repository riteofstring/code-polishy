package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/pack"
)

func handlePackMeta(invocation invocation) int {
	if len(invocation.arguments) == 0 {
		return commandUsageError("pack", "pack requires install, verify, or root")
	}
	action, arguments := invocation.arguments[0], invocation.arguments[1:]
	switch action {
	case "install":
		return installPack(arguments)
	case "verify":
		return verifyPack(arguments)
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

func verifyPack(arguments []string) int {
	source, err := packSourceOption("pack verify", arguments)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	result, err := pack.VerifySource(context.Background(), source, pack.DefaultRunner())
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
