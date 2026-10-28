package pythonfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/strictjson"
)

type VersionCheck struct {
	Specifier string `json:"specifier"`
	Version   string `json:"version"`
}

type VersionMatch struct {
	Specifier string `json:"specifier"`
	Version   string `json:"version"`
	Matches   bool   `json:"matches"`
	Error     string `json:"error"`
}

func MatchVersions(python string, checks []VersionCheck) ([]VersionMatch, error) {
	if !filepath.IsAbs(python) || len(checks) > maximumItems {
		return nil, fmt.Errorf("python-facts version checks need an absolute interpreter and at most %d items", maximumItems)
	}
	for _, check := range checks {
		if !utf8.ValidString(check.Specifier) || !utf8.ValidString(check.Version) {
			return nil, fmt.Errorf("python-facts version check contains invalid UTF-8")
		}
	}
	if checks == nil {
		checks = []VersionCheck{}
	}
	request := struct {
		Protocol  string         `json:"protocol"`
		Operation string         `json:"operation"`
		Checks    []VersionCheck `json:"checks"`
	}{Protocol, "version-specifiers", checks}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(data) > maximumRequestSize {
		return nil, fmt.Errorf("python-facts version checks exceed the byte limit")
	}
	output, err := runAdapter(context.Background(), python, data)
	if err != nil {
		return nil, err
	}
	return decodeVersionMatches(output, checks)
}

func decodeVersionMatches(data []byte, checks []VersionCheck) ([]VersionMatch, error) {
	var response struct {
		Protocol         string         `json:"protocol"`
		PythonVersion    string         `json:"pythonVersion"`
		PackagingVersion string         `json:"packagingVersion"`
		Matches          []VersionMatch `json:"matches"`
	}
	if err := strictjson.Decode(data, &response); err != nil {
		return nil, fmt.Errorf("decode python-facts version matches: %w", err)
	}
	if response.Protocol != Protocol || response.PackagingVersion != PackagingVersion || !strings.HasPrefix(response.PythonVersion, "3.12.") {
		return nil, fmt.Errorf("python-facts version matches have an invalid tool identity")
	}
	if len(response.Matches) != len(checks) {
		return nil, fmt.Errorf("python-facts version matches have an invalid result count")
	}
	for index, check := range checks {
		match := response.Matches[index]
		if match.Specifier != check.Specifier || match.Version != check.Version {
			return nil, fmt.Errorf("python-facts version matches reordered inputs")
		}
	}
	return response.Matches, nil
}
