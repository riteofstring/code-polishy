package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/release"
)

func handleInstallRustMeta(invocation invocation) int {
	flags := flag.NewFlagSet("install-rust", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("source", "", "local Rust companion archive")
	if err := flags.Parse(invocation.arguments); err != nil {
		return commandUsageError("install-rust", err.Error())
	}
	if flags.NArg() != 0 || invocation.configPath != "" {
		return commandUsageError("install-rust", "install-rust accepts only --source PATH")
	}
	if *source != "" {
		absolute, err := filepath.Abs(*source)
		if err != nil {
			return operationalError(err)
		}
		*source = absolute
	}
	if err := release.InstallRustBundle(context.Background(), invocation.repoRoot, invocation.policyRoot, *source); err != nil {
		return operationalError(err)
	}
	fmt.Println("Installed the selected release's verified Rust tools.")
	return 0
}
