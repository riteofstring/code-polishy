package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const taskSessionReceiptArtifact = "session.txt"

type requiredReceiptValue struct {
	name  string
	value string
	set   bool
}

func (value *requiredReceiptValue) String() string { return value.value }
func (value *requiredReceiptValue) Set(candidate string) error {
	if value.set {
		return fmt.Errorf("%s may be specified only once", value.name)
	}
	if strings.ContainsAny(candidate, "\r\n\x00") {
		return fmt.Errorf("%s must be a single receipt line", value.name)
	}
	value.value = candidate
	value.set = true
	return nil
}

type receiptValues []string

func (values *receiptValues) String() string { return strings.Join(*values, ",") }
func (values *receiptValues) Set(value string) error {
	if value == "" || strings.ContainsAny(value, ",\r\n\x00") {
		return errors.New("task-session receipt list values must be non-empty single values without commas")
	}
	*values = append(*values, value)
	return nil
}

type taskSessionReceiptOptions struct {
	outputDir              requiredReceiptValue
	status                 requiredReceiptValue
	sourceRoot             requiredReceiptValue
	sourceBranch           requiredReceiptValue
	trustedBase            requiredReceiptValue
	candidateHead          requiredReceiptValue
	config                 requiredReceiptValue
	promote                requiredReceiptValue
	policyBinary           requiredReceiptValue
	policyDigest           requiredReceiptValue
	environmentReceipt     requiredReceiptValue
	environmentDigest      requiredReceiptValue
	environmentPaths       requiredReceiptValue
	environmentPathsDigest requiredReceiptValue
	scopeDigest            requiredReceiptValue
	workspace              requiredReceiptValue
	commandDigest          requiredReceiptValue
	modules                receiptValues
	exactPaths             receiptValues
	newPaths               receiptValues
}

func handleTaskSessionReceiptMeta(invocation invocation) int {
	options, err := parseTaskSessionReceiptOptions(invocation.arguments)
	if err != nil {
		return commandUsageError("task-session-receipt", err.Error())
	}
	if err := writeTaskSessionReceipt(options); err != nil {
		return operationalError(err)
	}
	return 0
}

func parseTaskSessionReceiptOptions(arguments []string) (taskSessionReceiptOptions, error) {
	options := taskSessionReceiptOptions{}
	flags := flag.NewFlagSet("task-session-receipt", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	required := []struct {
		name  string
		value *requiredReceiptValue
	}{
		{"output-dir", &options.outputDir},
		{"status", &options.status},
		{"source-root", &options.sourceRoot},
		{"source-branch", &options.sourceBranch},
		{"trusted-base", &options.trustedBase},
		{"candidate-head", &options.candidateHead},
		{"config", &options.config},
		{"promote", &options.promote},
		{"policy-binary", &options.policyBinary},
		{"policy-digest", &options.policyDigest},
		{"environment-receipt", &options.environmentReceipt},
		{"environment-digest", &options.environmentDigest},
		{"environment-paths", &options.environmentPaths},
		{"environment-paths-digest", &options.environmentPathsDigest},
		{"scope-digest", &options.scopeDigest},
		{"workspace", &options.workspace},
		{"command-digest", &options.commandDigest},
	}
	for index := range required {
		required[index].value.name = "--" + required[index].name
		flags.Var(required[index].value, required[index].name, "task-session receipt value")
	}
	flags.Var(&options.modules, "module", "frozen task module")
	flags.Var(&options.exactPaths, "exact-path", "frozen tracked path")
	flags.Var(&options.newPaths, "new-path", "frozen new path")
	if err := flags.Parse(arguments); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, errors.New("task-session-receipt accepts no positional arguments")
	}
	for _, option := range required {
		if !option.value.set {
			return options, fmt.Errorf("task-session-receipt requires --%s", option.name)
		}
	}
	if len(options.modules) == 0 {
		return options, errors.New("task-session-receipt requires at least one --module")
	}
	resolved, err := filepath.EvalSymlinks(options.outputDir.value)
	if err != nil {
		return options, fmt.Errorf("resolve task-session receipt directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return options, fmt.Errorf("task-session receipt directory does not exist: %s", options.outputDir.value)
	}
	options.outputDir.value = resolved
	return options, nil
}

func writeTaskSessionReceipt(options taskSessionReceiptOptions) error {
	fields := []struct{ key, value string }{
		{"version", "5"},
		{"status", options.status.value},
		{"sourceRoot", options.sourceRoot.value},
		{"sourceBranch", options.sourceBranch.value},
		{"trustedBase", options.trustedBase.value},
		{"candidateHead", options.candidateHead.value},
		{"config", options.config.value},
		{"promote", options.promote.value},
		{"policyBinary", options.policyBinary.value},
		{"policyDigest", options.policyDigest.value},
		{"environmentReceipt", options.environmentReceipt.value},
		{"environmentDigest", options.environmentDigest.value},
		{"environmentPaths", options.environmentPaths.value},
		{"environmentPathsDigest", options.environmentPathsDigest.value},
		{"scopeDigest", options.scopeDigest.value},
		{"modules", strings.Join(options.modules, ",")},
		{"exactPaths", strings.Join(options.exactPaths, ",")},
		{"newPaths", strings.Join(options.newPaths, ",")},
		{"workspace", options.workspace.value},
		{"commandDigest", options.commandDigest.value},
	}
	var receipt bytes.Buffer
	for _, field := range fields {
		fmt.Fprintf(&receipt, "%s=%s\n", field.key, field.value)
	}
	temporary, err := os.CreateTemp(options.outputDir.value, ".task-session-receipt-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(receipt.Bytes()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, filepath.Join(options.outputDir.value, taskSessionReceiptArtifact))
}
