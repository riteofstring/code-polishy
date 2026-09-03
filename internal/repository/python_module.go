package repository

import (
	"path/filepath"
	"strings"
)

func PythonModuleName(project PythonProject, source string) (module, packageName string) {
	base := pythonPackageCandidateBase(project, source)
	relative, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(source))
	if err != nil {
		return "", ""
	}
	relative = filepath.ToSlash(relative)
	extension := filepath.Ext(relative)
	if extension != ".py" && extension != ".pyi" {
		return "", ""
	}
	relative = strings.TrimSuffix(relative, extension)
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == ".." {
		return "", ""
	}
	packageParts := parts[:len(parts)-1]
	if parts[len(parts)-1] == "__init__" {
		parts = packageParts
	}
	return strings.Join(parts, "."), strings.Join(packageParts, ".")
}
