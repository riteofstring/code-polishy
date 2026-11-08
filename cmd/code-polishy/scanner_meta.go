package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/release"
)

func handleScannerUpdate(invocation invocation) int {
	flags := flag.NewFlagSet("scanner-update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("manifest", "", "signed manifest HTTPS URL")
	digest := flags.String("sha256", "", "signed manifest SHA-256")
	apply := flags.Bool("apply", false, "adopt the verified manifest")
	if err := flags.Parse(invocation.arguments); err != nil || flags.NArg() != 0 || *source == "" || *digest == "" {
		return commandUsageError("scanner-update", "requires --manifest URL --sha256 DIGEST [--apply]")
	}
	lock, present, err := release.ReadLock(invocation.repoRoot)
	if err != nil {
		return operationalError(err)
	}
	if !present {
		return operationalError(errors.New("scanner-update requires an installed release lock"))
	}
	signed, manifest, err := release.AcquireScanner(context.Background(), invocation.policyRoot, *source, *digest)
	if err == nil {
		err = artifactsecurity.ValidateScannerManifest(manifest)
	}
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("SCANNER %s Trivy %s %s contract %s\nMANIFEST %s\n", manifest.Version, manifest.TrivyVersion, manifest.SourceImage, manifest.Contract, signed.Payload)
	if *apply {
		if err := release.WriteScannerLock(invocation.repoRoot, invocation.policyRoot, lock, signed); err != nil {
			return operationalError(err)
		}
		fmt.Println("PASS scanner manifest adopted")
	} else {
		fmt.Println("PREVIEW: rerun with --apply to adopt this exact manifest")
	}
	return 0
}

func handleScannerSign(invocation invocation) int {
	flags := flag.NewFlagSet("scanner-sign", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := flags.String("input", "", "manifest payload JSON")
	key := flags.String("key", "", "PKCS8 PEM private key path")
	output := flags.String("output", "", "signed manifest output path")
	if err := flags.Parse(invocation.arguments); err != nil || flags.NArg() != 0 || *input == "" || *key == "" || *output == "" {
		return commandUsageError("scanner-sign", "requires --input PATH --key PATH --output PATH")
	}
	if err := signScannerFile(invocation.policyRoot, *input, *key, *output); err != nil {
		return operationalError(err)
	}
	return 0
}

func signScannerFile(policyRoot, input, key, output string) error {
	payload, err := os.ReadFile(input)
	if err != nil || len(payload) > release.MaximumScannerBytes {
		return errors.New("cannot read bounded scanner manifest payload")
	}
	private, err := os.ReadFile(key)
	if err != nil {
		return errors.New("cannot read signing key")
	}
	signed, err := release.SignScanner(payload, private)
	if err != nil {
		return err
	}
	manifest, err := release.VerifyScanner(policyRoot, &signed)
	if err == nil {
		err = artifactsecurity.ValidateScannerManifest(manifest)
	}
	if err != nil {
		return err
	}
	data, _ := json.Marshal(signed)
	return os.WriteFile(output, append(data, '\n'), 0o644)
}
