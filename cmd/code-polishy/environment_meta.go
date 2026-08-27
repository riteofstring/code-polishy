package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleGovernedEnvironmentMeta(invocation invocation) int {
	flags := flag.NewFlagSet("governed-environment", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "", "external environment receipt path")
	if err := flags.Parse(invocation.arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *output == "" {
		return usageError("governed-environment requires --output and no positional arguments")
	}
	policyEngine, err := engine.Open(invocation.repoRoot, invocation.policyRoot, invocation.configPath)
	if err != nil {
		return operationalError(err)
	}
	receipt, err := engine.FreezeGovernedEnvironment(policyEngine.Repository)
	if err != nil {
		return operationalError(err)
	}
	data, err := engine.MarshalGovernedEnvironment(receipt)
	if err != nil {
		return operationalError(err)
	}
	if err := writePrivateFile(*output, append(data, '\n')); err != nil {
		return operationalError(err)
	}
	for _, path := range receipt.PathEntries {
		fmt.Println(path)
	}
	return 0
}

func writePrivateFile(path string, data []byte) error {
	if path == "" {
		return errors.New("output path must not be empty")
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
