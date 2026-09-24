package quality

import (
	"bytes"
	"context"
	"errors"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func rustFormatWrite(ctx context.Context, repo repository.Repository, selected []string, commandRunner runner.Runner) []policy.Finding {
	files := editableFiles(repo, languageFiles(repo, repo.NativeAnalysisFiles(selected, "format", ""), "rust"))
	if len(files) == 0 {
		return nil
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return []policy.Finding{rustFormatFinding("repository", err.Error())}
	}
	plan := rustfacts.PlanDiscovery(repo, files, allFiles)
	discovery := rustfacts.Discover(ctx, repo, plan, commandRunner)
	findings := append([]policy.Finding{}, discovery.Findings...)
	for _, path := range files {
		findings = append(findings, rustFormatFile(ctx, repo, path, discovery.Workspaces, commandRunner, true)...)
	}
	return findings
}

func rustFormatFindings(ctx context.Context, repo repository.Repository, selected []string, workspaces []rustfacts.Workspace) []policy.Finding {
	files := editableFiles(repo, languageFiles(repo, repo.NativeAnalysisFiles(selected, "format", ""), "rust"))
	findings := []policy.Finding{}
	for _, path := range files {
		findings = append(findings, rustFormatFile(ctx, repo, path, workspaces, runner.OSRunner{}, false)...)
	}
	return findings
}

func rustFormatFile(ctx context.Context, repo repository.Repository, path string, workspaces []rustfacts.Workspace, commandRunner runner.Runner, write bool) []policy.Finding {
	edition, err := rustfacts.SourceEdition(workspaces, path)
	if err != nil {
		return []policy.Finding{rustFormatFinding(path, err.Error())}
	}
	input, err := repo.ReadContainedFile(path, 8<<20)
	if err != nil {
		return []policy.Finding{rustFormatFinding(path, err.Error())}
	}
	formatted, err := rustfacts.FormatSource(ctx, repo, path, edition, input, commandRunner)
	if err != nil {
		return []policy.Finding{rustFormatFinding(path, err.Error())}
	}
	if bytes.Equal(input, formatted) {
		return nil
	}
	if !write {
		return []policy.Finding{rustFormatFinding(path, "file is not rustfmt formatted")}
	}
	if err := writeRustFormat(repo, path, input, formatted); err != nil {
		return []policy.Finding{rustFormatFinding(path, err.Error())}
	}
	return nil
}

func writeRustFormat(repo repository.Repository, path string, input, formatted []byte) error {
	current, err := repo.ReadContainedFile(path, 8<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, input) {
		return errors.New("rust source changed during formatting; no output was written")
	}
	return repo.WriteRegularFile(path, formatted)
}

func rustFormatFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "quality.rustfmt", Path: path, Subject: "format", Message: message}
}

func rustToolFindings(repo repository.Repository, files []string) []policy.Finding {
	for _, path := range files {
		if repo.Language(path) != "rust" || !repo.NativeAnalysis(path, "format") {
			continue
		}
		if err := rustfacts.ToolProblem(repo); err != nil {
			return []policy.Finding{toolFinding("rust", err.Error())}
		}
		return nil
	}
	return nil
}
