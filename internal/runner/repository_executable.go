package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var pythonInterpreterName = regexp.MustCompile(`^python(?:3(?:\.[0-9]+)?)?$`)
var pythonInterpreterPin = regexp.MustCompile(`^3\.[0-9]+\.[0-9]+$`)

func RepositoryExecutable(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("repository executable must be relative: %s", relative)
	}
	resolved, err := containedPath(root, relative)
	if err == nil {
		if !ExecutableFile(resolved) {
			return "", fmt.Errorf("repository executable is missing, non-regular, or non-executable: %s", relative)
		}
		return resolved, nil
	}
	directory := filepath.Dir(filepath.FromSlash(relative))
	if filepath.Base(directory) == "bin" && filepath.Base(filepath.Dir(directory)) == ".venv" && pythonInterpreterName.MatchString(filepath.Base(relative)) {
		return virtualenvInterpreter(root, relative, filepath.Dir(directory))
	}
	return "", err
}

func virtualenvInterpreter(root, relative, environment string) (string, error) {
	bin, err := containedPath(root, filepath.Join(environment, "bin"))
	if err != nil {
		return "", err
	}
	home, err := virtualenvHome(root, environment)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(bin, filepath.Base(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !ExecutableFile(resolved) {
		return "", fmt.Errorf("managed Python interpreter is missing or non-executable: %s", relative)
	}
	homeInterpreter, err := filepath.EvalSymlinks(filepath.Join(home, filepath.Base(resolved)))
	if !filepath.IsAbs(home) || err != nil || resolved != homeInterpreter {
		return "", fmt.Errorf("managed Python interpreter does not match the interpreter in its declared pyvenv.cfg home: %s", relative)
	}
	return candidate, nil
}

func virtualenvHome(root, environment string) (string, error) {
	configuration, err := executableConfiguration(root, filepath.Join(environment, "pyvenv.cfg"))
	if err != nil {
		return "", fmt.Errorf("managed Python environment: %w", err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(configuration, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if _, duplicate := fields[key]; duplicate {
			return "", fmt.Errorf("managed Python environment repeats %q", key)
		}
		fields[key] = strings.TrimSpace(value)
	}
	pin, err := executableConfiguration(root, filepath.Join(filepath.Dir(environment), ".python-version"))
	if err != nil {
		return "", fmt.Errorf("managed Python environment requires an exact project .python-version: %w", err)
	}
	pin = strings.TrimSpace(pin)
	version := fields["version_info"]
	if version == "" {
		version = fields["version"]
	}
	if !pythonInterpreterPin.MatchString(pin) || version != pin || fields["version"] != "" && fields["version"] != pin {
		return "", fmt.Errorf("managed Python environment version %q does not match exact .python-version %q", version, pin)
	}
	return fields["home"], nil
}

func executableConfiguration(root, relative string) (string, error) {
	path, err := containedPath(root, relative)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("environment configuration must be a regular file: %s", relative)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return "", fmt.Errorf("environment configuration is unreadable or exceeds 4096 bytes: %s", relative)
	}
	return string(data), nil
}
