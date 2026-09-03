package repository

import (
	"fmt"
	pathpkg "path"
	"strings"
)

type PythonBuildBackend struct {
	Module string
	Object string
	Line   int
}

type PythonProjectPath struct {
	Path string
	Line int
}

func pythonProjectBuildMetadata(manifest string, assignments []pythonTOMLAssignment) (PythonBuildBackend, []PythonProjectPath, error) {
	backend := PythonBuildBackend{}
	paths := []PythonProjectPath{}
	backendFound := false
	pathsFound := false
	for _, leaf := range pythonTOMLLeaves(assignments) {
		switch strings.Join(leaf.path, ".") {
		case "build-system.build-backend":
			if backendFound {
				return PythonBuildBackend{}, nil, pythonManifestError(manifest, leaf.value.line, "build-system.build-backend is declared more than once")
			}
			backendFound = true
			parsed, err := parsePythonBuildBackend(leaf.value)
			if err != nil {
				return PythonBuildBackend{}, nil, pythonManifestError(manifest, leaf.value.line, "invalid build-system.build-backend: %v", err)
			}
			backend = parsed
		case "build-system.backend-path":
			if pathsFound {
				return PythonBuildBackend{}, nil, pythonManifestError(manifest, leaf.value.line, "build-system.backend-path is declared more than once")
			}
			pathsFound = true
			parsed, err := parsePythonBackendPaths(leaf.value)
			if err != nil {
				return PythonBuildBackend{}, nil, pythonManifestError(manifest, leaf.value.line, "invalid build-system.backend-path: %v", err)
			}
			paths = parsed
		}
	}
	return backend, paths, nil
}

func parsePythonBuildBackend(value pythonTOMLValue) (PythonBuildBackend, error) {
	if value.kind != pythonTOMLString {
		return PythonBuildBackend{}, fmt.Errorf("must be a string")
	}
	module, object, found := strings.Cut(value.text, ":")
	if strings.Contains(object, ":") || !validPythonEntryPointIdentifierChain(module) || found && !validPythonEntryPointIdentifierChain(object) {
		return PythonBuildBackend{}, fmt.Errorf("must be an exact module or module:object reference")
	}
	return PythonBuildBackend{Module: module, Object: object, Line: value.line}, nil
}

func parsePythonBackendPaths(value pythonTOMLValue) ([]PythonProjectPath, error) {
	if value.kind != pythonTOMLArray {
		return nil, fmt.Errorf("must be an array of relative directory strings")
	}
	paths := make([]PythonProjectPath, 0, len(value.array))
	seen := map[string]bool{}
	for _, item := range value.array {
		if item.kind != pythonTOMLString || !validPythonBackendPath(item.text) {
			return nil, fmt.Errorf("must contain only normalized relative directory paths")
		}
		if seen[item.text] {
			return nil, fmt.Errorf("contains duplicate path %q", item.text)
		}
		seen[item.text] = true
		paths = append(paths, PythonProjectPath{Path: item.text, Line: item.line})
	}
	return paths, nil
}

func validPythonBackendPath(value string) bool {
	return value != "" && !strings.Contains(value, "\\") && !strings.HasPrefix(value, "/") &&
		value != ".." && !strings.HasPrefix(value, "../") && pathpkg.Clean(value) == value
}
