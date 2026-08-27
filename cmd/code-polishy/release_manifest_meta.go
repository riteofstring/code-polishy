package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/release"
)

type releaseManifestOptions struct{ root, revision, source, destination string }

func handleReleaseManifestMeta(invocation invocation) int {
	if invocation.configPath != "" || len(invocation.arguments) == 0 {
		return usageError("release-manifest requires write or verify and accepts no global configuration")
	}
	mode := invocation.arguments[0]
	flags := flag.NewFlagSet("release-manifest "+mode, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := releaseManifestOptions{}
	flags.StringVar(&options.root, "root", "", "staged or installed release root")
	flags.StringVar(&options.revision, "source-revision", "", "exact reviewed source commit")
	flags.StringVar(&options.source, "source", "", "closed source tree to materialize")
	flags.StringVar(&options.destination, "destination", "", "new dereferenced destination tree")
	if err := flags.Parse(invocation.arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return usageError("release-manifest accepts no positional arguments after its mode")
	}
	switch mode {
	case "write":
		return writeReleaseManifest(options)
	case "verify":
		return verifyReleaseManifest(options)
	case "materialize":
		return materializeReleaseManifest(options)
	default:
		return usageError("release-manifest requires write or verify")
	}
}

func writeReleaseManifest(options releaseManifestOptions) int {
	if options.root == "" || options.revision == "" || options.source != "" || options.destination != "" {
		return usageError("release-manifest write requires --root PATH and --source-revision COMMIT")
	}
	manifest, err := release.WriteManifest(options.root, options.revision)
	if err != nil {
		return operationalError(err)
	}
	fmt.Println(manifest.ReleaseDigest)
	return 0
}

func verifyReleaseManifest(options releaseManifestOptions) int {
	if options.root == "" || options.revision != "" || options.source != "" || options.destination != "" {
		return usageError("release-manifest verify requires only --root PATH")
	}
	manifest, present, err := release.ReadManifest(options.root)
	if err != nil {
		return operationalError(err)
	}
	if !present {
		return operationalError(fmt.Errorf("%s has no %s", options.root, release.ManifestFilename))
	}
	if err := manifest.Verify(options.root); err != nil {
		return operationalError(err)
	}
	fmt.Printf("Verified Code Polishy %s for %s (%s)\n", manifest.CodePolishyVersion, manifest.Host, manifest.ReleaseDigest)
	return 0
}

func materializeReleaseManifest(options releaseManifestOptions) int {
	if options.source == "" || options.destination == "" || options.root != "" || options.revision != "" {
		return usageError("release-manifest materialize requires --source PATH and --destination PATH")
	}
	if err := release.CopyTreeDereferenced(options.source, options.destination); err != nil {
		return operationalError(err)
	}
	return 0
}
