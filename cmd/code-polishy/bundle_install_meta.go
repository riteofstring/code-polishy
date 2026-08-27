package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/release"
)

func handleInstallBundleMeta(invocation invocation) int {
	flags := flag.NewFlagSet("install-bundle", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("source", "", "absolute path to a locally built native release zip")
	digest := flags.String("sha256", "", "expected release zip SHA-256")
	prefix := flags.String("prefix", "", "installation prefix")
	if err := flags.Parse(invocation.arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *source == "" || *digest == "" || *prefix == "" {
		return usageError("install-bundle requires --source, --sha256, and --prefix")
	}
	manifest, err := release.InstallLocalBundle(*source, *digest, *prefix)
	if err != nil {
		return operationalError(err)
	}
	if manifest.ReleaseDigest == "" {
		return operationalError(errors.New("installed release has no identity"))
	}
	fmt.Printf("PASS installed Code Polishy %s %s for %s\n", manifest.CodePolishyVersion, manifest.ReleaseDigest, manifest.Host)
	return 0
}
