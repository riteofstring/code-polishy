package supplychain

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func readSignedGitEvidence(repo repository.Repository, declaration policy.GitEvidenceAttestation, provider policy.GitEvidenceProvider) (gitEvidenceStatement, string, error) {
	data, err := readGitEvidenceFile(repo, declaration.Path, maximumGitEvidenceBytes)
	if err != nil {
		return gitEvidenceStatement{}, "", err
	}
	statement, err := verifySignedGitEvidence(data, provider)
	return statement, gitEvidenceDigest(data), err
}

func verifySignedGitEvidence(data []byte, provider policy.GitEvidenceProvider) (gitEvidenceStatement, error) {
	var envelope gitEvidenceEnvelope
	if err := decodeGitEvidenceJSON(data, &envelope); err != nil {
		return gitEvidenceStatement{}, err
	}
	if envelope.PayloadType != gitEvidencePayloadType || len(envelope.Signatures) != 1 {
		return gitEvidenceStatement{}, gitEvidenceFailure("invalid", "Git evidence requires the supported DSSE payload type and exactly one signature")
	}
	payload, err := canonicalGitEvidenceBase64(envelope.Payload)
	if err != nil {
		return gitEvidenceStatement{}, err
	}
	if err := verifyGitEvidenceSignature(provider, envelope.Signatures[0], payload); err != nil {
		return gitEvidenceStatement{}, err
	}
	var statement gitEvidenceStatement
	if err := decodeGitEvidenceJSON(payload, &statement); err != nil {
		return gitEvidenceStatement{}, err
	}
	if statement.Protocol != "git-evidence/v1" || statement.Issuer != provider.Issuer || statement.PolicySHA256 != provider.PolicySHA256 {
		return gitEvidenceStatement{}, gitEvidenceFailure("untrusted", "Git evidence issuer or assessment policy does not match configured trust")
	}
	return statement, nil
}

func verifyGitEvidenceSignature(provider policy.GitEvidenceProvider, signature gitEvidenceSignature, payload []byte) error {
	key, err := canonicalGitEvidenceBase64(provider.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || signature.KeyID != provider.Name {
		return gitEvidenceFailure("untrusted", "Git evidence does not identify its configured signing key")
	}
	decoded, err := canonicalGitEvidenceBase64(signature.Sig)
	if err != nil || len(decoded) != ed25519.SignatureSize {
		return gitEvidenceFailure("untrusted", "Git evidence signature is malformed")
	}
	if !ed25519.Verify(key, gitEvidencePAE(payload), decoded) {
		return gitEvidenceFailure("untrusted", "Git evidence signature cannot be verified against configured trust")
	}
	return nil
}

func gitEvidencePAE(payload []byte) []byte {
	prefix := fmt.Sprintf("DSSEv1 %d %s %d ", len(gitEvidencePayloadType), gitEvidencePayloadType, len(payload))
	return append([]byte(prefix), payload...)
}

func canonicalGitEvidenceBase64(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != value {
		return nil, gitEvidenceFailure("invalid", "Git evidence contains noncanonical base64")
	}
	return decoded, nil
}

func decodeGitEvidenceJSON(data []byte, destination any) error {
	if len(data) == 0 || len(data) > maximumGitEvidenceBytes || !utf8.Valid(data) {
		return gitEvidenceFailure("invalid", "Git evidence must be bounded UTF-8 JSON")
	}
	if err := validateGitEvidenceJSON(data, destination); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence does not match the supported document structure")
	}
	if err := requireOneJSONValue(decoder); err != nil {
		return gitEvidenceFailure("invalid", "Git evidence must contain exactly one JSON document")
	}
	return nil
}

func readGitEvidenceFile(repo repository.Repository, path string, maximum int64) ([]byte, error) {
	normalized, err := repo.NormalizePath(path)
	managed := strings.HasPrefix(path, ".code-polishy-reports/git-evidence/")
	if err != nil || normalized != path || repo.IsExcluded(path) && !managed {
		return nil, gitEvidenceFailure("invalid", "Git evidence artifact must be a canonical governed path or a managed Git evidence report")
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return nil, gitEvidenceFailure("unavailable", "Git evidence repository is unavailable")
	}
	defer root.Close()
	info, err := gitEvidenceFileInfo(root, path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maximum {
		return nil, gitEvidenceFailure("invalid", "Git evidence artifact exceeds its byte limit")
	}
	file, err := root.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, gitEvidenceFailure("unavailable", "Git evidence artifact could not be opened")
	}
	defer file.Close()
	return readStableGitEvidence(file, info, maximum)
}

func gitEvidenceFileInfo(root *os.Root, path string) (os.FileInfo, error) {
	segments := strings.Split(path, "/")
	var info os.FileInfo
	for index := range segments {
		var err error
		info, err = root.Lstat(filepath.Join(segments[:index+1]...))
		if err != nil {
			return nil, gitEvidenceFailure("unavailable", "Git evidence artifact is unavailable; export it from the authorized CI provider")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, gitEvidenceFailure("invalid", "Git evidence artifact cannot traverse symbolic links")
		}
	}
	if !info.Mode().IsRegular() {
		return nil, gitEvidenceFailure("invalid", "Git evidence artifact must be a regular file")
	}
	return info, nil
}

func readStableGitEvidence(file *os.File, expected os.FileInfo, maximum int64) ([]byte, error) {
	before, err := file.Stat()
	if err != nil || !os.SameFile(expected, before) || !before.Mode().IsRegular() {
		return nil, gitEvidenceFailure("invalid", "Git evidence artifact changed during opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, gitEvidenceFailure("unavailable", "Git evidence artifact could not be read")
	}
	after, err := file.Stat()
	if err != nil || int64(len(data)) > maximum || before.Size() != int64(len(data)) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, gitEvidenceFailure("invalid", "Git evidence artifact changed during reading or exceeded its byte limit")
	}
	return data, nil
}
