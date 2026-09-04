package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/release"
)

type releaseManifestOptions struct {
	root, revision, source, destination, archive, output, descriptor, template string
	descriptors                                                                []string
}

type repeatedReleaseDescriptor []string

func (values *repeatedReleaseDescriptor) String() string { return fmt.Sprint([]string(*values)) }

func (values *repeatedReleaseDescriptor) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func handleReleaseManifestMeta(invocation invocation) int {
	if invocation.configPath != "" || len(invocation.arguments) == 0 {
		return commandUsageError("release-manifest", "release-manifest requires an operation and accepts no global configuration")
	}
	mode := invocation.arguments[0]
	flags := flag.NewFlagSet("release-manifest "+mode, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := releaseManifestOptions{}
	flags.StringVar(&options.root, "root", "", "staged or installed release root")
	flags.StringVar(&options.revision, "source-revision", "", "exact reviewed source commit")
	flags.StringVar(&options.source, "source", "", "closed source tree to materialize")
	flags.StringVar(&options.destination, "destination", "", "new dereferenced destination tree")
	flags.StringVar(&options.archive, "archive", "", "verified release archive")
	flags.StringVar(&options.output, "output", "", "new release archive or index")
	flags.StringVar(&options.descriptor, "descriptor", "", "release publication descriptor")
	flags.StringVar(&options.template, "template", "", "OCI Containerfile template")
	repeated := repeatedReleaseDescriptor{}
	flags.Var(&repeated, "artifact-descriptor", "host publication descriptor included in an index")
	if err := flags.Parse(invocation.arguments[1:]); err != nil {
		return commandUsageError("release-manifest", err.Error())
	}
	if flags.NArg() != 0 {
		return commandUsageError("release-manifest", "release-manifest accepts no positional arguments after its mode")
	}
	options.descriptors = repeated
	switch mode {
	case "write":
		return writeReleaseManifest(options)
	case "verify":
		return verifyReleaseManifest(options)
	case "materialize":
		return materializeReleaseManifest(options)
	default:
		return handleReleasePublicationMeta(mode, options)
	}
}

func handleReleasePublicationMeta(mode string, options releaseManifestOptions) int {
	switch mode {
	case "archive":
		return archiveReleaseManifest(options)
	case "publish":
		return publishReleaseManifest(options)
	case "index":
		return indexReleaseManifest(options)
	case "oci-context":
		return ociContextReleaseManifest(options)
	default:
		return commandUsageError("release-manifest", "release-manifest requires write, verify, materialize, archive, publish, index, or oci-context")
	}
}

func releaseManifestOptionsEmpty(options releaseManifestOptions, allowed ...string) bool {
	set := map[string]bool{}
	for _, name := range allowed {
		set[name] = true
	}
	values := map[string]string{
		"root": options.root, "revision": options.revision, "source": options.source,
		"destination": options.destination, "archive": options.archive, "output": options.output,
		"descriptor": options.descriptor, "template": options.template,
	}
	for name, value := range values {
		if !set[name] && value != "" {
			return false
		}
	}
	return set["descriptors"] || len(options.descriptors) == 0
}

func writeReleaseManifest(options releaseManifestOptions) int {
	if options.root == "" || options.revision == "" || !releaseManifestOptionsEmpty(options, "root", "revision") {
		return commandUsageError("release-manifest", "release-manifest write requires --root PATH and --source-revision COMMIT")
	}
	manifest, err := release.WriteManifest(options.root, options.revision)
	if err != nil {
		return operationalError(err)
	}
	fmt.Println(manifest.ReleaseDigest)
	return 0
}

func verifyReleaseManifest(options releaseManifestOptions) int {
	if options.root == "" || !releaseManifestOptionsEmpty(options, "root") {
		return commandUsageError("release-manifest", "release-manifest verify requires only --root PATH")
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
	if options.source == "" || options.destination == "" || !releaseManifestOptionsEmpty(options, "source", "destination") {
		return commandUsageError("release-manifest", "release-manifest materialize requires --source PATH and --destination PATH")
	}
	if err := release.CopyTreeDereferenced(options.source, options.destination); err != nil {
		return operationalError(err)
	}
	return 0
}

func archiveReleaseManifest(options releaseManifestOptions) int {
	if options.root == "" || options.output == "" || !releaseManifestOptionsEmpty(options, "root", "output") {
		return commandUsageError("release-manifest", "release-manifest archive requires --root PATH and --output PATH")
	}
	digest, err := release.WriteArchive(options.root, options.output)
	if err != nil {
		return operationalError(err)
	}
	fmt.Println(digest)
	return 0
}

func publishReleaseManifest(options releaseManifestOptions) int {
	if options.archive == "" || options.destination == "" || !releaseManifestOptionsEmpty(options, "archive", "destination") {
		return commandUsageError("release-manifest", "release-manifest publish requires --archive PATH and --destination PATH")
	}
	artifact, err := release.PublishArchive(options.archive, options.destination)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("Published Code Polishy %s for %s at %s\n", artifact.CodePolishyVersion, artifact.Host, options.destination)
	return 0
}

func indexReleaseManifest(options releaseManifestOptions) int {
	if options.output == "" || len(options.descriptors) == 0 || !releaseManifestOptionsEmpty(options, "output", "descriptors") {
		return commandUsageError("release-manifest", "release-manifest index requires repeated --artifact-descriptor PATH and --output PATH")
	}
	index, err := release.WritePublicationIndex(options.descriptors, options.output)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("Indexed Code Polishy %s for %d hosts at %s\n", index.CodePolishyVersion, len(index.Artifacts), options.output)
	return 0
}

func ociContextReleaseManifest(options releaseManifestOptions) int {
	if options.descriptor == "" || options.template == "" || options.destination == "" ||
		!releaseManifestOptionsEmpty(options, "descriptor", "template", "destination") {
		return commandUsageError("release-manifest", "release-manifest oci-context requires --descriptor PATH, --template PATH, and --destination PATH")
	}
	manifest, err := release.PrepareOCIContext(options.descriptor, options.template, options.destination)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("Prepared Code Polishy %s for %s at %s\n", manifest.CodePolishyVersion, manifest.Host, options.destination)
	return 0
}
