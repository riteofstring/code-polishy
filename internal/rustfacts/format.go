package rustfacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func FormatSource(ctx context.Context, repo repository.Repository, path, edition string, source []byte, commandRunner runner.Runner) ([]byte, error) {
	configuration, style, err := formatConfiguration(repo, path, edition)
	if err != nil {
		return nil, err
	}
	command := Command(repo, "policy-rustfmt-"+path, "rustfmt", "--edition", edition, "--style-edition", style, "--emit", "stdout", "--config-path", configuration)
	command.Stdin = source
	_, output, err := runner.RunCaptured(ctx, commandRunner, repo.Root, command, runner.CaptureStructured)
	if err != nil {
		return nil, fmt.Errorf("rustfmt failed for %s: %v %s", path, err, diagnosticOutput(output.Stderr))
	}
	if len(output.Stderr) > 0 {
		return nil, fmt.Errorf("rustfmt reported a configuration or formatting problem for %s: %s", path, diagnosticOutput(output.Stderr))
	}
	if !utf8.Valid(output.Stdout) {
		return nil, errors.New("rustfmt returned non-UTF-8 source")
	}
	return output.Stdout, nil
}

func formatConfiguration(repo repository.Repository, path, edition string) (string, string, error) {
	if !supportedEdition(edition) {
		return "", "", fmt.Errorf("unsupported Rust formatting edition %q", edition)
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		for _, name := range []string{".rustfmt.toml", "rustfmt.toml"} {
			candidate := filepath.ToSlash(filepath.Join(parent, name))
			if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(candidate))); errors.Is(err, os.ErrNotExist) {
				continue
			}
			data, err := repo.ReadContainedFile(candidate, 1<<20)
			if err != nil {
				return "", "", err
			}
			style, err := formatStyle(data, edition)
			if err != nil {
				return "", "", fmt.Errorf("rustfmt configuration %s: %w", candidate, err)
			}
			return filepath.Join(repo.Root, filepath.FromSlash(candidate)), style, nil
		}
		if parent == "." || filepath.Dir(parent) == parent {
			break
		}
	}
	return filepath.Join(repo.PolicyRoot, "tools", "rustfmt.toml"), edition, nil
}

func formatStyle(data []byte, edition string) (string, error) {
	var settings map[string]any
	if err := toml.Unmarshal(data, &settings); err != nil {
		return "", err
	}
	for _, name := range []string{"disable_all_formatting", "ignore", "skip_children", "skip_macro_invocations", "unstable_features"} {
		if _, present := settings[name]; present {
			return "", fmt.Errorf("setting %s cannot weaken governed source formatting", name)
		}
	}
	if declared, exists := settings["edition"]; exists && declared != edition {
		return "", fmt.Errorf("formatting edition must match Cargo target edition %s", edition)
	}
	if style, exists := settings["style_edition"]; exists {
		text, ok := style.(string)
		if !ok || !supportedEdition(text) {
			return "", errors.New("unsupported Rust style edition")
		}
		return text, nil
	}
	return edition, nil
}

func SourceEdition(workspaces []Workspace, path string) (string, error) {
	if edition, err := targetEdition(workspaces, path); edition != "" || err != nil {
		return edition, err
	}
	selected := sourcePackage(workspaces, path)
	if selected == nil {
		return "", errors.New("selected Rust source has no discovered Cargo package or target")
	}
	for _, target := range selected.Targets {
		if target.Edition != selected.Edition {
			return "", errors.New("source edition is ambiguous across Cargo targets; resolved module ownership is required")
		}
	}
	return selected.Edition, nil
}

func sourcePackage(workspaces []Workspace, path string) *Package {
	selected := (*Package)(nil)
	selectedSize := -1
	for _, workspace := range workspaces {
		for index := range workspace.Packages {
			pkg := &workspace.Packages[index]
			root := filepath.ToSlash(filepath.Dir(pkg.Manifest))
			size := len(root)
			if root == "." {
				size = 0
			}
			if (root == "." || strings.HasPrefix(path, root+"/")) && size > selectedSize {
				selected, selectedSize = pkg, size
			}
		}
	}
	return selected
}

func targetEdition(workspaces []Workspace, path string) (string, error) {
	edition := ""
	for _, workspace := range workspaces {
		for _, pkg := range workspace.Packages {
			for _, target := range pkg.Targets {
				if target.Path != path {
					continue
				}
				if edition != "" && edition != target.Edition {
					return "", errors.New("selected source belongs to Cargo targets with different Rust editions")
				}
				edition = target.Edition
			}
		}
	}
	return edition, nil
}
