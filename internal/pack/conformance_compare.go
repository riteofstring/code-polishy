package pack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const maximumConformanceDifferences = 128
const maximumConformanceDifferenceBytes = 1024

type conformanceDifferenceCollector struct {
	differences []ConformanceDifference
	err         error
}

type conformanceComparisonRoots struct {
	repository string
	policy     string
	data       string
}

func compareConformanceRuns(reference ConformanceRunEvidence, referenceRoots conformanceComparisonRoots, candidate ConformanceRunEvidence, candidateRoots conformanceComparisonRoots, variantPaths ...map[string]bool) ([]ConformanceDifference, error) {
	variants := map[string]bool{}
	if len(variantPaths) > 0 {
		variants = variantPaths[0]
	}
	referenceValue, err := conformanceComparableValue(reference, referenceRoots, variants)
	if err != nil {
		return nil, fmt.Errorf("normalize reference: %w", err)
	}
	candidateValue, err := conformanceComparableValue(candidate, candidateRoots, variants)
	if err != nil {
		return nil, fmt.Errorf("normalize candidate: %w", err)
	}
	collector := conformanceDifferenceCollector{differences: []ConformanceDifference{}}
	collector.compare("", referenceValue, candidateValue)
	return collector.differences, collector.err
}

func conformanceComparableValue(run ConformanceRunEvidence, roots conformanceComparisonRoots, variantPaths map[string]bool) (any, error) {
	report, err := decodeConformanceValue(run.Report)
	if err != nil {
		return nil, err
	}
	head := ""
	if len(variantPaths) > 0 {
		head = run.BeforeGit.Head
	}
	report = normalizeConformanceValue(report, roots, head, "")
	beforeFiles := normalizeConformanceFileIdentities(run.Before, variantPaths)
	afterFiles := normalizeConformanceFileIdentities(run.After, variantPaths)
	beforeGitIdentity := normalizeConformanceGitIdentity(run.BeforeGit, len(variantPaths) > 0)
	afterGitIdentity := normalizeConformanceGitIdentity(run.AfterGit, len(variantPaths) > 0)
	before, err := encodeConformanceValue(beforeFiles)
	if err != nil {
		return nil, err
	}
	after, err := encodeConformanceValue(afterFiles)
	if err != nil {
		return nil, err
	}
	beforeGit, err := encodeConformanceValue(beforeGitIdentity)
	if err != nil {
		return nil, err
	}
	afterGit, err := encodeConformanceValue(afterGitIdentity)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exitStatus": run.ExitStatus,
		"report":     report,
		"stderr":     normalizeConformanceString(run.Stderr, roots, head),
		"before":     before,
		"after":      after,
		"beforeGit":  beforeGit,
		"afterGit":   afterGit,
	}, nil
}

func normalizeConformanceFileIdentities(files []ConformanceFileIdentity, variants map[string]bool) []ConformanceFileIdentity {
	result := append([]ConformanceFileIdentity{}, files...)
	for index := range result {
		if variants[result[index].Path] {
			result[index].SHA256 = "$LANE_OVERRIDE"
		}
	}
	return result
}

func normalizeConformanceGitIdentity(identity ConformanceGitIdentity, variant bool) ConformanceGitIdentity {
	if variant {
		identity.Head = "$LANE_HEAD"
	}
	return identity
}

func decodeConformanceValue(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func encodeConformanceValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeConformanceValue(data)
}

func normalizeConformanceValue(value any, roots conformanceComparisonRoots, head, path string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := path + "/" + escapeConformancePointer(key)
			if dropConformanceField(path, key) {
				continue
			}
			result[key] = normalizeConformanceValue(child, roots, head, childPath)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for index, child := range typed {
			result = append(result, normalizeConformanceValue(child, roots, head, path+"/"+strconv.Itoa(index)))
		}
		if sortConformanceArray(path) {
			slices.SortFunc(result, func(left, right any) int {
				leftJSON, _ := json.Marshal(left)
				rightJSON, _ := json.Marshal(right)
				return bytes.Compare(leftJSON, rightJSON)
			})
		}
		return result
	case string:
		return normalizeConformanceString(typed, roots, head)
	default:
		return value
	}
}

func dropConformanceField(parent, key string) bool {
	if parent == "" && key == "reportPath" {
		return true
	}
	if key == "invocationId" || key == "runId" {
		return true
	}
	if strings.HasPrefix(parent, "/execution") {
		return key == "evaluationDurationMilliseconds" || key == "durationMilliseconds" || key == "resourceWaitMilliseconds" || key == "hits" || key == "misses" || key == "builds"
	}
	return false
}

func sortConformanceArray(path string) bool {
	normalized := conformancePointerShape(path)
	return slices.Contains([]string{
		"/analysisContext",
		"/analysisContext/*/paths",
		"/findings",
		"/notes",
		"/sourceDependencyGraph/edges",
		"/sourceDependencyGraph/nodes",
		"/suppressed",
	}, normalized)
}

func conformancePointerShape(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if _, err := strconv.Atoi(part); err == nil && part != "" {
			parts[index] = "*"
		}
	}
	return strings.Join(parts, "/")
}

func normalizeConformanceString(value string, roots conformanceComparisonRoots, heads ...string) string {
	result := value
	for _, item := range []struct {
		value       string
		replacement string
	}{{roots.repository, "$REPOSITORY_ROOT"}, {roots.policy, "$POLICY_ROOT"}, {roots.data, "$PACK_DATA_ROOT"}} {
		for _, replacement := range []string{item.value, filepath.ToSlash(item.value)} {
			if replacement != "" {
				result = strings.ReplaceAll(result, replacement, item.replacement)
			}
		}
	}
	for _, head := range heads {
		if head != "" {
			result = strings.ReplaceAll(result, head, "$LANE_HEAD")
		}
	}
	return result
}

func (collector *conformanceDifferenceCollector) compare(path string, reference, candidate any) {
	if collector.err != nil || reflect.DeepEqual(reference, candidate) {
		return
	}
	if len(collector.differences) >= maximumConformanceDifferences {
		collector.err = fmt.Errorf("comparison exceeds %d differences", maximumConformanceDifferences)
		return
	}
	referenceMap, referenceIsMap := reference.(map[string]any)
	candidateMap, candidateIsMap := candidate.(map[string]any)
	if referenceIsMap && candidateIsMap {
		keys := make([]string, 0, len(referenceMap)+len(candidateMap))
		seen := map[string]bool{}
		for key := range referenceMap {
			seen[key] = true
			keys = append(keys, key)
		}
		for key := range candidateMap {
			if !seen[key] {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			left, leftExists := referenceMap[key]
			right, rightExists := candidateMap[key]
			childPath := path + "/" + escapeConformancePointer(key)
			if !leftExists || !rightExists {
				collector.add(childPath, conformanceMissing(left, leftExists), conformanceMissing(right, rightExists))
				continue
			}
			collector.compare(childPath, left, right)
		}
		return
	}
	referenceArray, referenceIsArray := reference.([]any)
	candidateArray, candidateIsArray := candidate.([]any)
	if referenceIsArray && candidateIsArray {
		maximum := max(len(referenceArray), len(candidateArray))
		for index := 0; index < maximum; index++ {
			childPath := path + "/" + strconv.Itoa(index)
			if index >= len(referenceArray) || index >= len(candidateArray) {
				var left, right any = "<missing>", "<missing>"
				if index < len(referenceArray) {
					left = referenceArray[index]
				}
				if index < len(candidateArray) {
					right = candidateArray[index]
				}
				collector.add(childPath, left, right)
				continue
			}
			collector.compare(childPath, referenceArray[index], candidateArray[index])
		}
		return
	}
	collector.add(path, reference, candidate)
}

func conformanceMissing(value any, exists bool) any {
	if !exists {
		return "<missing>"
	}
	return value
}

func (collector *conformanceDifferenceCollector) add(path string, reference, candidate any) {
	if collector.err != nil {
		return
	}
	if path == "" {
		path = "/"
	}
	left, err := conformanceDifferenceValue(reference)
	if err != nil {
		collector.err = err
		return
	}
	right, err := conformanceDifferenceValue(candidate)
	if err != nil {
		collector.err = err
		return
	}
	collector.differences = append(collector.differences, ConformanceDifference{Path: path, Reference: left, Candidate: right})
}

func conformanceDifferenceValue(value any) (string, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	data := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	if len(data) > maximumConformanceDifferenceBytes {
		return "", fmt.Errorf("comparison value at a differing path exceeds %d bytes", maximumConformanceDifferenceBytes)
	}
	return string(data), nil
}

func escapeConformancePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
