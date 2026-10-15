package rustfacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func configurationProblem(repo repository.Repository) error {
	if err := executionDirectories(repo); err != nil {
		return err
	}
	for parent := filepath.Dir(repo.Root); ; parent = filepath.Dir(parent) {
		for _, name := range []string{"config", "config.toml"} {
			path := filepath.Join(parent, ".cargo", name)
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("cargo ancestor configuration %s is outside governed repository inputs", path)
			}
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	for _, name := range []string{".cargo/config", ".cargo/config.toml"} {
		if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(name))); errors.Is(err, os.ErrNotExist) {
			continue
		}
		data, err := repo.ReadContainedFile(name, 1<<20)
		if err != nil {
			return err
		}
		var settings map[string]any
		if err := toml.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("cargo configuration %s: %w", name, err)
		}
		if err := validateSettings(settings); err != nil {
			return fmt.Errorf("cargo configuration %s: %w", name, err)
		}
	}
	return nil
}

func validateSettings(settings map[string]any) error {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		switch key {
		case "alias", "term":
			continue
		case "build", "net":
			fields, ok := settings[key].(map[string]any)
			if !ok {
				return fmt.Errorf("%s must be a table", key)
			}
			for field := range fields {
				if !slices.Contains([]string{"build.target", "build.target-dir", "build.jobs", "net.offline"}, key+"."+field) {
					return fmt.Errorf("setting %s.%s needs governed Rust execution support", key, field)
				}
			}
		default:
			return fmt.Errorf("setting %s needs governed Rust execution support", key)
		}
	}
	return nil
}

func compilationTargetConfiguration(repo repository.Repository) error {
	for _, name := range []string{".cargo/config", ".cargo/config.toml"} {
		if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(name))); errors.Is(err, os.ErrNotExist) {
			continue
		}
		data, err := repo.ReadContainedFile(name, 1<<20)
		if err != nil {
			return err
		}
		var settings struct {
			Build struct {
				Target any `toml:"target"`
			} `toml:"build"`
		}
		if err := toml.Unmarshal(data, &settings); err != nil {
			return err
		}
		if target := settings.Build.Target; target != nil && !nativeTargetSetting(target) {
			return fmt.Errorf("cargo configuration %s requests target %v; governed compilation currently supports only native target %s", name, target, HostTarget())
		}
	}
	return nil
}

func nativeTargetSetting(value any) bool {
	if list, ok := value.([]any); ok {
		return len(list) == 1 && list[0] == HostTarget()
	}
	return value == HostTarget()
}

func manifestPathProblem(repo repository.Repository, manifest repository.CargoManifest) error {
	paths := []string{}
	if manifest.Package != nil && manifest.Package.Workspace != "" {
		paths = append(paths, manifest.Package.Workspace)
	}
	if manifest.Workspace != nil {
		paths = append(paths, manifest.Workspace.Members...)
		paths = append(paths, manifest.Workspace.Exclude...)
		paths = append(paths, manifest.Workspace.DefaultMembers...)
	}
	for _, path := range paths {
		if strings.ContainsAny(path, "\\\x00:") || filepath.IsAbs(path) {
			return fmt.Errorf("cargo workspace path %q must be repository-relative", path)
		}
		joined := filepath.Join(manifest.Root, filepath.FromSlash(path), "Cargo.toml")
		if _, err := repo.NormalizePath(filepath.ToSlash(joined)); err != nil {
			return fmt.Errorf("cargo workspace path %q escapes the repository", path)
		}
	}
	return nil
}
