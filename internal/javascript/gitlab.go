package javascript

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type GitLabResult struct {
	Controls    []string        `json:"controls"`
	Images      []GitLabImage   `json:"images"`
	Includes    []GitLabInclude `json:"includes"`
	Unsupported []Unsupported   `json:"unsupported"`
}

type GitLabImage struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Image string `json:"image"`
}

type GitLabInclude struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Local     string `json:"local"`
	Project   string `json:"project"`
	File      string `json:"file"`
	Ref       string `json:"ref"`
	Remote    string `json:"remote"`
	Integrity string `json:"integrity"`
	Component string `json:"component"`
	Template  string `json:"template"`
}

type gitLabResultWire struct {
	Controls    *[]string            `json:"controls"`
	Images      *[]gitLabImageWire   `json:"images"`
	Includes    *[]gitLabIncludeWire `json:"includes"`
	Unsupported *[]Unsupported       `json:"unsupported"`
}

type gitLabImageWire struct {
	Path  *string `json:"path"`
	Scope *string `json:"scope"`
	Image *string `json:"image"`
}

type gitLabIncludeWire struct {
	Path      *string `json:"path"`
	Kind      *string `json:"kind"`
	Local     *string `json:"local"`
	Project   *string `json:"project"`
	File      *string `json:"file"`
	Ref       *string `json:"ref"`
	Remote    *string `json:"remote"`
	Integrity *string `json:"integrity"`
	Component *string `json:"component"`
	Template  *string `json:"template"`
}

func (bundle Bundle) GitLab(ctx context.Context, root string, paths, governedPaths []string) (GitLabResult, error) {
	if err := validateGitLabRoots(paths); err != nil {
		return GitLabResult{}, err
	}
	if err := validateGitLabGovernedPaths(governedPaths); err != nil {
		return GitLabResult{}, err
	}
	payload, err := fileRequest(OperationGitLab, root, paths)
	if err != nil {
		return GitLabResult{}, err
	}
	payload.GovernedPaths = governedPaths
	result, err := bundle.exchange(ctx, payload, gitLabTimeout)
	if err != nil {
		return GitLabResult{}, err
	}
	reported, err := decodeGitLabResult(result, paths, governedPaths)
	if err != nil {
		return GitLabResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable %s result: %w", OperationGitLab, err)
	}
	return reported, nil
}

func validateGitLabRoots(paths []string) error {
	if len(paths) == 0 || len(paths) > 2 {
		return fmt.Errorf("the %s request must select one or two root GitLab configuration files", OperationGitLab)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !gitLabRootPath(path) || seen[path] {
			return fmt.Errorf("the %s request must select distinct root GitLab configuration files", OperationGitLab)
		}
		seen[path] = true
	}
	return nil
}

func gitLabRootPath(path string) bool {
	return path == ".gitlab-ci.yml" || path == ".gitlab-ci.yaml"
}

func validateGitLabGovernedPaths(paths []string) error {
	if len(paths) > maximumGitLabGovernedPaths {
		return fmt.Errorf("the %s request declares %d governed paths, more than the %d limit", OperationGitLab, len(paths), maximumGitLabGovernedPaths)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !containedPath(path) || seen[path] {
			return fmt.Errorf("the %s request declares invalid governed path %q", OperationGitLab, path)
		}
		seen[path] = true
	}
	return nil
}

func decodeGitLabResult(data []byte, requested, governedPaths []string) (GitLabResult, error) {
	var wire gitLabResultWire
	if err := decodeExactly(data, &wire); err != nil {
		return GitLabResult{}, err
	}
	if err := validateGitLabResultWire(wire); err != nil {
		return GitLabResult{}, err
	}
	controls, err := decodeGitLabControls(*wire.Controls, requested, governedPaths)
	if err != nil {
		return GitLabResult{}, err
	}
	images, err := decodeGitLabImages(*wire.Images, controls)
	if err != nil {
		return GitLabResult{}, err
	}
	includes, err := decodeGitLabIncludes(*wire.Includes, controls)
	if err != nil {
		return GitLabResult{}, err
	}
	unsupported, err := decodeGitLabUnsupported(*wire.Unsupported, controls)
	if err != nil {
		return GitLabResult{}, err
	}
	return GitLabResult{Controls: *wire.Controls, Images: images, Includes: includes, Unsupported: unsupported}, nil
}

func validateGitLabResultWire(wire gitLabResultWire) error {
	if wire.Controls == nil || wire.Images == nil || wire.Includes == nil || wire.Unsupported == nil {
		return fmt.Errorf("the gitlab result is missing required fields")
	}
	counts := []int{len(*wire.Controls), len(*wire.Images), len(*wire.Includes), len(*wire.Unsupported)}
	for _, count := range counts {
		if count > maximumOperationPaths {
			return fmt.Errorf("the gitlab result exceeds the %d fact limit", maximumOperationPaths)
		}
	}
	return nil
}

func decodeGitLabControls(reported, requested, governedPaths []string) (map[string]bool, error) {
	controls := map[string]bool{}
	governed := stringSet(governedPaths)
	requestedRoots := stringSet(requested)
	for _, path := range reported {
		if !validGitLabControl(path, controls, governed, requestedRoots) {
			return nil, fmt.Errorf("the gitlab result reports invalid control path %q", path)
		}
		controls[path] = true
	}
	for _, path := range requested {
		if !controls[path] {
			return nil, fmt.Errorf("the gitlab result omits requested root %q from its controls", path)
		}
	}
	return controls, nil
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func validGitLabControl(path string, controls, governed, requested map[string]bool) bool {
	return containedPath(path) && !controls[path] && (governed[path] || requested[path])
}

func decodeGitLabImages(wires []gitLabImageWire, controls map[string]bool) ([]GitLabImage, error) {
	images := make([]GitLabImage, 0, len(wires))
	for index, wire := range wires {
		image, err := decodeGitLabImage(wire, controls)
		if err != nil {
			return nil, fmt.Errorf("the gitlab result image %d is invalid: %w", index, err)
		}
		images = append(images, image)
	}
	return images, nil
}

func decodeGitLabIncludes(wires []gitLabIncludeWire, controls map[string]bool) ([]GitLabInclude, error) {
	includes := make([]GitLabInclude, 0, len(wires))
	for index, wire := range wires {
		include, err := decodeGitLabInclude(wire, controls)
		if err != nil {
			return nil, fmt.Errorf("the gitlab result include %d is invalid: %w", index, err)
		}
		includes = append(includes, include)
	}
	return includes, nil
}

func decodeGitLabUnsupported(wires []Unsupported, controls map[string]bool) ([]Unsupported, error) {
	unsupported := make([]Unsupported, 0, len(wires))
	for index, wire := range wires {
		if !controls[wire.Path] || !validGitLabText(wire.Reason, 4096) {
			return nil, fmt.Errorf("the gitlab result unsupported entry %d is invalid", index)
		}
		unsupported = append(unsupported, wire)
	}
	return unsupported, nil
}

func decodeGitLabImage(wire gitLabImageWire, controls map[string]bool) (GitLabImage, error) {
	if wire.Path == nil || wire.Scope == nil || wire.Image == nil {
		return GitLabImage{}, fmt.Errorf("it is missing required fields")
	}
	if !controls[*wire.Path] || !validGitLabText(*wire.Scope, 1024) || !validGitLabText(*wire.Image, 4096) {
		return GitLabImage{}, fmt.Errorf("it declares invalid path, scope, or image")
	}
	return GitLabImage{Path: *wire.Path, Scope: *wire.Scope, Image: *wire.Image}, nil
}

func decodeGitLabInclude(wire gitLabIncludeWire, controls map[string]bool) (GitLabInclude, error) {
	include, err := gitLabIncludeFromWire(wire)
	if err != nil {
		return GitLabInclude{}, err
	}
	if !controls[include.Path] || !validGitLabText(include.Kind, 64) {
		return GitLabInclude{}, fmt.Errorf("it declares an invalid path or kind")
	}
	if !validGitLabIncludeText(include) {
		return GitLabInclude{}, fmt.Errorf("it declares invalid text")
	}
	if !validGitLabIncludeShape(include) {
		return GitLabInclude{}, fmt.Errorf("it declares an invalid %q include shape", include.Kind)
	}
	if include.Kind == "local" && !validLocalGitLabInclude(include, controls) {
		return GitLabInclude{}, fmt.Errorf("it declares a local include outside the governed controls")
	}
	return include, nil
}

func gitLabIncludeFromWire(wire gitLabIncludeWire) (GitLabInclude, error) {
	fields := []*string{wire.Path, wire.Kind, wire.Local, wire.Project, wire.File, wire.Ref, wire.Remote, wire.Integrity, wire.Component, wire.Template}
	for _, field := range fields {
		if field == nil {
			return GitLabInclude{}, fmt.Errorf("it is missing required fields")
		}
	}
	return GitLabInclude{
		Path: *wire.Path, Kind: *wire.Kind, Local: *wire.Local, Project: *wire.Project,
		File: *wire.File, Ref: *wire.Ref, Remote: *wire.Remote, Integrity: *wire.Integrity, Component: *wire.Component, Template: *wire.Template,
	}, nil
}

func validGitLabIncludeText(include GitLabInclude) bool {
	fields := []string{include.Local, include.Project, include.File, include.Ref, include.Remote, include.Integrity, include.Component, include.Template}
	for _, field := range fields {
		if field != "" && !validGitLabText(field, 4096) {
			return false
		}
	}
	return true
}

type gitLabIncludeShape struct {
	required []string
	allowed  []string
}

func validGitLabIncludeShape(include GitLabInclude) bool {
	shapes := map[string]gitLabIncludeShape{
		"local":     {required: []string{"local"}, allowed: []string{"local"}},
		"project":   {required: []string{"project", "file"}, allowed: []string{"project", "file", "ref"}},
		"remote":    {required: []string{"remote"}, allowed: []string{"remote", "integrity"}},
		"component": {required: []string{"component"}, allowed: []string{"component"}},
		"template":  {required: []string{"template"}, allowed: []string{"template"}},
	}
	shape, exists := shapes[include.Kind]
	if !exists {
		return false
	}
	values := map[string]string{
		"local": include.Local, "project": include.Project, "file": include.File, "ref": include.Ref,
		"remote": include.Remote, "integrity": include.Integrity, "component": include.Component, "template": include.Template,
	}
	return gitLabFieldsMatch(values, shape)
}

func gitLabFieldsMatch(values map[string]string, shape gitLabIncludeShape) bool {
	for name, value := range values {
		if slices.Contains(shape.required, name) && value == "" {
			return false
		}
		if !slices.Contains(shape.allowed, name) && value != "" {
			return false
		}
	}
	return true
}

func validLocalGitLabInclude(include GitLabInclude, controls map[string]bool) bool {
	return containedPath(include.Local) && controls[include.Local]
}

func validGitLabText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && !strings.ContainsAny(value, "\x00\r\n")
}
