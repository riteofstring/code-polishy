package finalstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	EvidenceVersion       = 1
	ContextLineRadius     = 20
	MaximumSourceBytes    = 2 << 20
	MaximumFindingBytes   = 1024
	MaximumFindings       = 256
	KindMetaNote          = "meta-note"
	KindCorrectionResidue = "correction-residue"
	KindUnknown           = "unknown-final-state"
)

type PathRole string

const (
	RoleProductionSource PathRole = "production-source"
	RoleTest             PathRole = "test"
	RoleDocumentation    PathRole = "current-state-documentation"
	RoleProductInput     PathRole = "product-input"
	RolePlan             PathRole = "plan"
	RoleChangelog        PathRole = "changelog"
	RoleGenerated        PathRole = "generated"
	RoleFixture          PathRole = "fixture"
)

type BuildInput struct {
	Path   string
	Role   PathRole
	Patch  []byte
	Source []byte
}

type Evidence struct {
	Version int            `json:"version"`
	Paths   []PathEvidence `json:"paths"`
	SHA256  string         `json:"sha256"`
}

type PathEvidence struct {
	Path  string         `json:"path"`
	Role  PathRole       `json:"role"`
	Hunks []HunkEvidence `json:"hunks"`
}

type HunkEvidence struct {
	ID             string        `json:"id"`
	SHA256         string        `json:"sha256"`
	CandidateStart int           `json:"candidate_start"`
	CandidateLines int           `json:"candidate_lines"`
	Patch          string        `json:"patch"`
	Context        SourceContext `json:"source_context"`
}

type SourceContext struct {
	Available bool   `json:"available"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256"`
}

type Finding struct {
	Kind            string   `json:"kind"`
	Path            string   `json:"path"`
	Line            int      `json:"line"`
	PatchHunkSHA256 string   `json:"patch_hunk_sha256"`
	IntentIDs       []string `json:"intent_ids"`
	Summary         string   `json:"summary"`
}

func Build(inputs []BuildInput) (Evidence, error) {
	if inputs == nil {
		return Evidence{}, errors.New("final-state evidence inputs are missing")
	}
	ordered := append([]BuildInput{}, inputs...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Path < ordered[right].Path })
	paths := make([]PathEvidence, 0, len(ordered))
	previous := ""
	for _, input := range ordered {
		if !validPath(input.Path) || input.Path <= previous || !validRole(input.Role) {
			return Evidence{}, fmt.Errorf("invalid final-state evidence path %q", input.Path)
		}
		hunks, err := buildHunks(input)
		if err != nil {
			return Evidence{}, err
		}
		paths = append(paths, PathEvidence{Path: input.Path, Role: input.Role, Hunks: hunks})
		previous = input.Path
	}
	evidence := Evidence{Version: EvidenceVersion, Paths: paths}
	digest, err := evidenceDigest(evidence)
	if err != nil {
		return Evidence{}, err
	}
	evidence.SHA256 = digest
	return evidence, nil
}

func ValidateEvidence(evidence Evidence) error {
	if err := validateEvidenceHeader(evidence); err != nil {
		return err
	}
	if err := validateEvidencePaths(evidence.Paths); err != nil {
		return err
	}
	digest, err := evidenceDigest(evidence)
	if err != nil || digest != evidence.SHA256 {
		return errors.New("final-state evidence digest is invalid")
	}
	return nil
}

func validateEvidenceHeader(evidence Evidence) error {
	if evidence.Version != EvidenceVersion || evidence.Paths == nil || !validSHA256(evidence.SHA256) {
		return errors.New("final-state evidence header is invalid")
	}
	return nil
}

func validateEvidencePaths(paths []PathEvidence) error {
	previousPath := ""
	seenHunks := map[string]bool{}
	for _, path := range paths {
		if !validPath(path.Path) || path.Path <= previousPath || !validRole(path.Role) || path.Hunks == nil {
			return errors.New("final-state path evidence is invalid")
		}
		if err := validatePathHunks(path, seenHunks); err != nil {
			return err
		}
		previousPath = path.Path
	}
	return nil
}

func validatePathHunks(path PathEvidence, seen map[string]bool) error {
	for _, hunk := range path.Hunks {
		if err := validateHunk(path.Path, hunk, seen); err != nil {
			return err
		}
		seen[hunk.SHA256] = true
	}
	return nil
}

func ValidateFindings(findings []Finding, evidence Evidence, intentIDs []string) error {
	if findings == nil {
		return errors.New("final_state_findings must be an explicit array")
	}
	if len(findings) > MaximumFindings {
		return fmt.Errorf("final_state_findings exceeds %d entries", MaximumFindings)
	}
	if err := ValidateEvidence(evidence); err != nil {
		return err
	}
	knownIntents := make(map[string]bool, len(intentIDs))
	for _, id := range intentIDs {
		knownIntents[id] = true
	}
	hunks := evidenceHunks(evidence)
	seen := map[string]bool{}
	for index, finding := range findings {
		if err := validateFinding(finding, hunks, knownIntents); err != nil {
			return fmt.Errorf("final-state finding %d is invalid: %w", index+1, err)
		}
		keyData, _ := json.Marshal(finding)
		key := string(keyData)
		if seen[key] {
			return fmt.Errorf("final-state finding %d duplicates an earlier finding", index+1)
		}
		seen[key] = true
	}
	return nil
}

func buildHunks(input BuildInput) ([]HunkEvidence, error) {
	if !utf8.Valid(input.Patch) {
		return nil, fmt.Errorf("final-state patch for %s is not UTF-8", input.Path)
	}
	if len(input.Source) > MaximumSourceBytes {
		input.Source = nil
	}
	lines := strings.SplitAfter(string(input.Patch), "\n")
	hunks := []HunkEvidence{}
	for index := 0; index < len(lines); index++ {
		if !strings.HasPrefix(lines[index], "@@ ") {
			continue
		}
		start, count, err := parseCandidateRange(lines[index])
		if err != nil {
			return nil, fmt.Errorf("parse final-state patch hunk for %s: %w", input.Path, err)
		}
		end := index + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "@@ ") && !strings.HasPrefix(lines[end], "diff --git ") {
			end++
		}
		patch := strings.Join(lines[index:end], "")
		digest := hunkDigest(input.Path, patch)
		hunks = append(hunks, HunkEvidence{
			ID: "hunk-" + digest[:16], SHA256: digest, CandidateStart: start, CandidateLines: count,
			Patch: patch, Context: sourceContext(input.Source, start, count),
		})
		index = end - 1
	}
	return hunks, nil
}

func parseCandidateRange(header string) (int, int, error) {
	fields := strings.Fields(header)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, errors.New("malformed unified diff header")
	}
	rangeValue := strings.TrimPrefix(fields[2], "+")
	parts := strings.Split(rangeValue, ",")
	if len(parts) > 2 {
		return 0, 0, errors.New("malformed candidate range")
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 0 {
		return 0, 0, errors.New("invalid candidate start line")
	}
	count := 1
	if len(parts) == 2 {
		count, err = strconv.Atoi(parts[1])
		if err != nil || count < 0 {
			return 0, 0, errors.New("invalid candidate line count")
		}
	}
	return start, count, nil
}

func sourceContext(source []byte, start, count int) SourceContext {
	if len(source) == 0 || !utf8.Valid(source) || count == 0 || start == 0 {
		return SourceContext{Available: false, SHA256: digestText("")}
	}
	lines := strings.Split(string(source), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if start > len(lines) {
		return SourceContext{Available: false, SHA256: digestText("")}
	}
	first := max(1, start-ContextLineRadius)
	last := min(len(lines), start+max(count, 1)-1+ContextLineRadius)
	content := strings.Join(lines[first-1:last], "\n")
	return SourceContext{Available: true, StartLine: first, EndLine: last, Content: content, SHA256: digestText(content)}
}

func validateHunk(path string, hunk HunkEvidence, seen map[string]bool) error {
	if hunk.CandidateStart < 0 || hunk.CandidateLines < 0 || !utf8.ValidString(hunk.Patch) {
		return errors.New("final-state patch hunk range is invalid")
	}
	digest := hunkDigest(path, hunk.Patch)
	if hunk.ID != "hunk-"+digest[:16] || hunk.SHA256 != digest || seen[hunk.SHA256] {
		return errors.New("final-state patch hunk identity is invalid")
	}
	return validateContext(hunk.Context)
}

func validateContext(context SourceContext) error {
	if !validSHA256(context.SHA256) || !utf8.ValidString(context.Content) || digestText(context.Content) != context.SHA256 {
		return errors.New("final-state source context digest is invalid")
	}
	if !context.Available {
		if context.StartLine != 0 || context.EndLine != 0 || context.Content != "" {
			return errors.New("unavailable final-state source context contains data")
		}
		return nil
	}
	if context.StartLine < 1 || context.EndLine < context.StartLine || strings.Count(context.Content, "\n") != context.EndLine-context.StartLine {
		return errors.New("final-state source context range is invalid")
	}
	return nil
}

func validateFinding(finding Finding, hunks map[string]struct {
	path string
	hunk HunkEvidence
}, intents map[string]bool) error {
	if err := validateFindingShape(finding); err != nil {
		return err
	}
	if err := validateFindingEvidence(finding, hunks); err != nil {
		return err
	}
	return validateFindingIntents(finding, intents)
}

func validateFindingShape(finding Finding) error {
	if !slices.Contains([]string{KindMetaNote, KindCorrectionResidue, KindUnknown}, finding.Kind) {
		return errors.New("unknown finding kind")
	}
	if !validPath(finding.Path) || finding.Line < 1 || !validFindingText(finding.Summary) || finding.IntentIDs == nil {
		return errors.New("finding fields are malformed")
	}
	return nil
}

func validateFindingEvidence(finding Finding, hunks map[string]struct {
	path string
	hunk HunkEvidence
}) error {
	bound, ok := hunks[finding.PatchHunkSHA256]
	if !ok || bound.path != finding.Path || !lineInHunkEvidence(finding.Line, bound.hunk) {
		return errors.New("finding does not cite packet evidence")
	}
	return nil
}

func validateFindingIntents(finding Finding, intents map[string]bool) error {
	if finding.Kind == KindCorrectionResidue && len(finding.IntentIDs) == 0 {
		return errors.New("correction residue needs an intent ID")
	}
	previous := ""
	for _, id := range finding.IntentIDs {
		if !intents[id] || id <= previous {
			return errors.New("finding intent IDs are unknown or not canonical")
		}
		previous = id
	}
	return nil
}

func lineInHunkEvidence(line int, hunk HunkEvidence) bool {
	if hunk.Context.Available {
		return line >= hunk.Context.StartLine && line <= hunk.Context.EndLine
	}
	return hunk.CandidateLines > 0 && line >= hunk.CandidateStart && line < hunk.CandidateStart+hunk.CandidateLines
}

func evidenceHunks(evidence Evidence) map[string]struct {
	path string
	hunk HunkEvidence
} {
	result := map[string]struct {
		path string
		hunk HunkEvidence
	}{}
	for _, path := range evidence.Paths {
		for _, hunk := range path.Hunks {
			result[hunk.SHA256] = struct {
				path string
				hunk HunkEvidence
			}{path: path.Path, hunk: hunk}
		}
	}
	return result
}

func evidenceDigest(evidence Evidence) (string, error) {
	material := evidence
	material.SHA256 = ""
	data, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	return digestText(string(data)), nil
}

func hunkDigest(path, patch string) string {
	data, _ := json.Marshal(struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}{Path: path, Patch: patch})
	return digestText(string(data))
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validRole(role PathRole) bool {
	return slices.Contains([]PathRole{
		RoleProductionSource, RoleTest, RoleDocumentation, RoleProductInput,
		RolePlan, RoleChangelog, RoleGenerated, RoleFixture,
	}, role)
}

func validPath(path string) bool {
	return path != "" && strings.TrimSpace(path) == path && !strings.HasPrefix(path, "/") &&
		!strings.HasPrefix(path, "../") && !strings.Contains(path, "/../") && !strings.ContainsAny(path, "\\\x00\r\n") && utf8.ValidString(path)
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validFindingText(value string) bool {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || len([]byte(value)) > MaximumFindingBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
