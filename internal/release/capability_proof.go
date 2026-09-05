package release

import (
	"fmt"
	"slices"
)

type capabilityCatalogProof struct {
	Release  Lock                       `json:"release"`
	Manifest capabilityManifestIdentity `json:"manifestIdentity"`
	Catalog  []byte                     `json:"catalogBytes"`
}

type capabilityManifestIdentity struct {
	Version                 int      `json:"version"`
	ReleaseVersion          string   `json:"releaseVersion"`
	SourceRevision          string   `json:"sourceRevision"`
	Features                []string `json:"features"`
	Tools                   Tools    `json:"tools"`
	CapabilityCatalogSHA256 string   `json:"capabilityCatalogSha256"`
}

func newCapabilityCatalogProof(locked Lock, manifest Manifest, data []byte) *capabilityCatalogProof {
	return &capabilityCatalogProof{
		Release: locked, Catalog: append([]byte{}, data...),
		Manifest: capabilityManifestIdentity{
			Version: manifest.ManifestVersion, ReleaseVersion: manifest.CodePolishyVersion,
			SourceRevision: manifest.SourceRevision, Features: append([]string{}, manifest.Features...),
			Tools: manifest.Tools, CapabilityCatalogSHA256: manifest.CapabilityCatalogSHA256,
		},
	}
}

func authenticateCapabilityProof(proof *capabilityCatalogProof, expected Lock) (AuthenticatedCapabilityCatalog, error) {
	if proof == nil || !sameCapabilityLock(proof.Release, expected) {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability evidence does not identify its exact release lock")
	}
	if _, err := parseLock(RenderLock(expected), "capability evidence release"); err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	identity := proof.Manifest
	manifest := Manifest{
		ManifestVersion: identity.Version, CodePolishyVersion: identity.ReleaseVersion,
		SourceRevision: identity.SourceRevision, Features: identity.Features, Tools: identity.Tools,
		CapabilityCatalogSHA256: identity.CapabilityCatalogSHA256,
	}
	if identity.Version != ManifestVersion || identity.ReleaseVersion != expected.CodePolishyVersion || manifest.Identity() != expected.ReleaseDigest {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability evidence does not authenticate its release identity")
	}
	if err := validateCapabilityProofIdentity(manifest, expected); err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	digest := capabilityContentSHA256(proof.Catalog)
	if digest != identity.CapabilityCatalogSHA256 {
		return AuthenticatedCapabilityCatalog{}, fmt.Errorf("capability evidence catalog does not match its authenticated digest")
	}
	catalog, err := ParseCapabilityCatalog(proof.Catalog)
	if err != nil {
		return AuthenticatedCapabilityCatalog{}, err
	}
	return AuthenticatedCapabilityCatalog{Release: expected, SHA256: digest, Catalog: catalog, proof: proof}, nil
}

func validateCapabilityProofIdentity(manifest Manifest, expected Lock) error {
	if !revisionPattern.MatchString(manifest.SourceRevision) {
		return fmt.Errorf("capability evidence records an invalid source revision")
	}
	if err := validateFeatures(manifest.Features); err != nil {
		return err
	}
	if err := validateManifestToolPins(manifest, "capability evidence"); err != nil {
		return err
	}
	for _, feature := range expected.Features {
		if !slices.Contains(manifest.Features, feature) {
			return fmt.Errorf("capability evidence does not supply locked feature %q", feature)
		}
	}
	return nil
}
