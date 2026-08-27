package artifactsecurity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type reportManifest struct {
	Version int                  `json:"version"`
	Files   []reportManifestFile `json:"files"`
}

type reportManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func writeArtifacts(repo repository.Repository, target policy.ArtifactTarget, scanned scanOutput) (string, error) {
	root, err := secureOutputRoot(repo)
	if err != nil {
		return "", err
	}
	runName := scanned.report.CompletedAt.UTC().Format("20060102T150405.000000000Z") + "-" + scanned.report.ArchiveSHA256[:12]
	directory := filepath.Join(root, target.Name, runName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", err
	}
	reportJSON, err := json.MarshalIndent(scanned.report, "", "  ")
	if err != nil {
		return "", err
	}
	reportJSON = append(reportJSON, '\n')
	markdown := []byte(renderMarkdown(scanned.report))
	files := []struct {
		name    string
		payload []byte
	}{
		{name: "report.json", payload: reportJSON},
		{name: "report.md", payload: markdown},
		{name: "sbom.cdx.json", payload: scanned.sbom},
	}
	manifest := reportManifest{Version: 1}
	for _, file := range files {
		if err := writeExclusive(filepath.Join(directory, file.name), file.payload); err != nil {
			return "", err
		}
		manifest.Files = append(manifest.Files, reportManifestFile{Name: file.name, SHA256: bytesSHA256(file.payload)})
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeExclusive(filepath.Join(directory, "manifest.json"), append(manifestJSON, '\n')); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(repo.Root, directory)
	if err != nil {
		return directory, nil
	}
	return filepath.ToSlash(relative), nil
}

func secureOutputRoot(repo repository.Repository) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return "", err
	}
	relative := repo.Config.SupplyChain.ArtifactSecurity.OutputDirectory
	candidate := filepath.Join(resolvedRoot, filepath.FromSlash(relative))
	if !inside(resolvedRoot, candidate) {
		return "", errors.New("artifact report output escapes the repository")
	}
	if err := validateOutputAncestor(resolvedRoot, candidate); err != nil {
		return "", err
	}
	if err := os.MkdirAll(candidate, 0o700); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !inside(resolvedRoot, resolved) {
		return "", errors.New("artifact report output resolves outside the repository")
	}
	return resolved, nil
}

func validateOutputAncestor(resolvedRoot, candidate string) error {
	ancestor := candidate
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
			if resolveErr != nil || !inside(resolvedRoot, resolvedAncestor) {
				return errors.New("artifact report output has an escaping symlink ancestor")
			}
			return nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("artifact report output has no contained existing ancestor")
		}
		ancestor = parent
	}
}

func writeExclusive(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func renderMarkdown(report artifactReport) string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "# Artifact security: %s\n\n", markdownText(report.Target))
	fmt.Fprintf(builder, "- Completed: %s\n", report.CompletedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(builder, "- Scanner: Trivy %s\n", markdownText(report.ScannerVersion))
	fmt.Fprintf(builder, "- Artifact reference: `%s`\n", markdownText(report.Reference))
	if report.ImageID != "" {
		fmt.Fprintf(builder, "- Image ID: `%s`\n", markdownText(report.ImageID))
	}
	fmt.Fprintf(builder, "- Artifact SHA-256: `%s`\n", report.ArchiveSHA256)
	fmt.Fprintf(builder, "- Database SHA-256: `%s`\n", report.DatabaseSHA256)
	fmt.Fprintf(builder, "- Blocking findings: %d\n", len(report.Findings))
	fmt.Fprintf(builder, "- Accepted OpenVEX findings: %d\n\n", len(report.AcceptedFindings))
	if len(report.Findings) == 0 {
		builder.WriteString("No HIGH or CRITICAL vulnerability, secret, misconfiguration, or end-of-life findings.\n")
	} else {
		builder.WriteString("## Blocking findings\n\n")
		for _, finding := range report.Findings {
			fmt.Fprintf(builder, "- **%s %s** — %s (`%s`)\n", markdownText(finding.Severity), markdownText(finding.Kind), markdownText(finding.ID), markdownText(finding.Target))
		}
	}
	if len(report.AcceptedFindings) > 0 {
		builder.WriteString("\n## Accepted OpenVEX findings\n\n")
		for _, accepted := range report.AcceptedFindings {
			fmt.Fprintf(builder, "- **%s** — %s: %s\n", markdownText(accepted.Finding.ID), markdownText(accepted.Justification), markdownText(accepted.ImpactStatement))
		}
	}
	return builder.String()
}

func markdownText(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "`", "'", "*", "\\*").Replace(value)
	return strings.TrimSpace(value)
}
