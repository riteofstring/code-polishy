package sourcegraph

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var providerIdentifier = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var providerVersion = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func normalizeProviderInput(input FactInput) (FactInput, error) {
	provider := *input.Provider
	if !validProviderIdentity(input, provider) {
		return FactInput{}, fmt.Errorf("source graph provider has an invalid analyzer identity")
	}
	root, err := normalizePath(input.Root, true)
	if err != nil {
		return FactInput{}, err
	}
	project, err := normalizePath(input.Project, false)
	if err != nil || !strings.HasPrefix(project, "pack/"+provider.Name+"/") {
		return FactInput{}, fmt.Errorf("source graph provider project does not match its declared identity")
	}
	if err := validateProviderDigests(input, provider); err != nil {
		return FactInput{}, err
	}
	provider.Languages, err = normalizeProviderLanguages(provider.Languages)
	if err != nil {
		return FactInput{}, err
	}
	if len(input.Paths) == 0 || len(input.Paths) > MaximumNodes {
		return FactInput{}, fmt.Errorf("source graph provider has an invalid source count")
	}
	input.Paths, err = normalizeFactPaths(input.Paths)
	if err != nil {
		return FactInput{}, err
	}
	input.Root, input.Project, input.Provider = root, project, &provider
	return input, nil
}

func validateProviderDigests(input FactInput, provider ProviderInput) error {
	for _, digest := range []string{provider.Digest, provider.InputsSHA256, provider.PolicySHA256, input.FactsSHA256, input.PartitionsSHA256, input.ResolutionSHA256} {
		if !factDigest(digest) {
			return fmt.Errorf("source graph provider has an invalid evidence digest")
		}
	}
	if provider.RuntimeSHA256 != "" && !factDigest(provider.RuntimeSHA256) {
		return fmt.Errorf("source graph provider has an invalid runtime digest")
	}
	return nil
}

func normalizeProviderLanguages(languages []string) ([]string, error) {
	if len(languages) == 0 || len(languages) > 32 {
		return nil, fmt.Errorf("source graph provider must declare its languages")
	}
	languages = slices.Clone(languages)
	slices.Sort(languages)
	for index, language := range languages {
		if !providerIdentifier.MatchString(language) || index > 0 && languages[index-1] == language {
			return nil, fmt.Errorf("source graph provider languages are invalid or duplicated")
		}
	}
	return languages, nil
}

func validProviderIdentity(input FactInput, provider ProviderInput) bool {
	return input.Analyzer == "pack" && input.Protocol == "code-polishy-pack/v2" && providerIdentifier.MatchString(provider.Name) && providerVersion.MatchString(provider.Version)
}
