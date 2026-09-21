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

func compareConformanceRuns(reference ConformanceRunEvidence, referenceRoot string, candidate ConformanceRunEvidence, candidateRoot string) ([]ConformanceDifference, error) {
	referenceValue, err := conformanceComparableValue(reference, referenceRoot)
	if err != nil {
		return nil, fmt.Errorf("normalize reference: %w", err)
	}
	candidateValue, err := conformanceComparableValue(candidate, candidateRoot)
	if err != nil {
		return nil, fmt.Errorf("normalize candidate: %w", err)
	}
	differences := []ConformanceDifference{}
	appendConformanceDifferences("", referenceValue, candidateValue, &differences)
	return differences, nil
}

func conformanceComparableValue(run ConformanceRunEvidence, root string) (any, error) {
	report, err := decodeConformanceValue(run.Report)
	if err != nil {
		return nil, err
	}
	report = normalizeConformanceValue(report, root, "")
	before, err := encodeConformanceValue(run.Before)
	if err != nil {
		return nil, err
	}
	after, err := encodeConformanceValue(run.After)
	if err != nil {
		return nil, err
	}
	beforeGit, err := encodeConformanceValue(run.BeforeGit)
	if err != nil {
		return nil, err
	}
	afterGit, err := encodeConformanceValue(run.AfterGit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exitStatus": run.ExitStatus,
		"report":     report,
		"stderr":     normalizeConformanceString(run.Stderr, root),
		"before":     before,
		"after":      after,
		"beforeGit":  beforeGit,
		"afterGit":   afterGit,
	}, nil
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

func normalizeConformanceValue(value any, root, path string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := path + "/" + escapeConformancePointer(key)
			if dropConformanceField(path, key) {
				continue
			}
			result[key] = normalizeConformanceValue(child, root, childPath)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for index, child := range typed {
			result = append(result, normalizeConformanceValue(child, root, path+"/"+strconv.Itoa(index)))
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
		return normalizeConformanceString(typed, root)
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

func normalizeConformanceString(value, root string) string {
	if root == "" {
		return value
	}
	replacements := []string{root, filepath.ToSlash(root)}
	result := value
	for _, replacement := range replacements {
		if replacement != "" {
			result = strings.ReplaceAll(result, replacement, "$REPOSITORY_ROOT")
		}
	}
	return result
}

func appendConformanceDifferences(path string, reference, candidate any, differences *[]ConformanceDifference) {
	if len(*differences) >= maximumConformanceDifferences || reflect.DeepEqual(reference, candidate) {
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
				appendConformanceDifference(childPath, conformanceMissing(left, leftExists), conformanceMissing(right, rightExists), differences)
				continue
			}
			appendConformanceDifferences(childPath, left, right, differences)
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
				appendConformanceDifference(childPath, left, right, differences)
				continue
			}
			appendConformanceDifferences(childPath, referenceArray[index], candidateArray[index], differences)
		}
		return
	}
	appendConformanceDifference(path, reference, candidate, differences)
}

func conformanceMissing(value any, exists bool) any {
	if !exists {
		return "<missing>"
	}
	return value
}

func appendConformanceDifference(path string, reference, candidate any, differences *[]ConformanceDifference) {
	if len(*differences) >= maximumConformanceDifferences {
		return
	}
	if path == "" {
		path = "/"
	}
	*differences = append(*differences, ConformanceDifference{Path: path, Reference: conformanceDifferenceValue(reference), Candidate: conformanceDifferenceValue(candidate)})
}

func conformanceDifferenceValue(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "<unavailable>"
	}
	if len(data) > maximumConformanceDifferenceBytes {
		return string(data[:maximumConformanceDifferenceBytes]) + "..."
	}
	return string(data)
}

func escapeConformancePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
