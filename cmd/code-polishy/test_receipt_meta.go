package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

type testReceiptOptions struct {
	mode   string
	output string
	source string
	sha256 string
}

func handleTestReceipts(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseTestReceiptOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	if options.mode == "export" {
		digest, count, err := testreceipt.ExportBundle(policyEngine.Repository.Root, options.output)
		if err != nil {
			return commandResult{}, err
		}
		message := fmt.Sprintf("Exported %d test receipts to %s (sha256 %s)", count, options.output, digest)
		return commandResult{quiet: true, messages: []string{message}}, nil
	}
	count, err := testreceipt.ImportBundle(policyEngine.Repository.Root, options.source, options.sha256)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{fmt.Sprintf("Imported %d authenticated CI test receipts", count)}}, nil
}

func parseTestReceiptOptions(arguments []string) (testReceiptOptions, error) {
	options := testReceiptOptions{}
	if len(arguments) == 0 || arguments[0] != "export" && arguments[0] != "import" {
		return options, fmt.Errorf("test-receipts requires export or import")
	}
	options.mode = arguments[0]
	flags := flag.NewFlagSet("test-receipts "+options.mode, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.output, "output", "", "new receipt bundle path")
	flags.StringVar(&options.source, "source", "", "authenticated receipt bundle path")
	flags.StringVar(&options.sha256, "sha256", "", "expected bundle SHA-256")
	if err := flags.Parse(arguments[1:]); err != nil {
		return options, err
	}
	if err := validateTestReceiptOptions(options, flags.NArg()); err != nil {
		return options, err
	}
	output, err := absoluteReceiptPath(options.output, "output")
	if err != nil {
		return options, err
	}
	source, err := absoluteReceiptPath(options.source, "source")
	if err != nil {
		return options, err
	}
	options.output = output
	options.source = source
	return options, nil
}

func validateTestReceiptOptions(options testReceiptOptions, positional int) error {
	if positional != 0 {
		return fmt.Errorf("test-receipts %s accepts no positional arguments", options.mode)
	}
	if options.mode == "export" && (options.output == "" || options.source != "" || options.sha256 != "") {
		return fmt.Errorf("test-receipts export requires only --output PATH")
	}
	if options.mode == "import" && (options.output != "" || options.source == "" || options.sha256 == "") {
		return fmt.Errorf("test-receipts import requires --source PATH and --sha256 DIGEST")
	}
	return nil
}

func absoluteReceiptPath(value, label string) (string, error) {
	if value == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve receipt bundle %s: %w", label, err)
	}
	return absolute, nil
}
