package javascript

import "testing"

func TestPeerResultsRequireCompleteScopedCoverage(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"covered":[],"findings":[],"unsupported":[]}`,
		`{"covered":["package.json"],"unsupported":[]}`,
		`{"covered":["package.json"],"findings":[{"path":"elsewhere/package.json","name":"react","message":"mismatch"}],"unsupported":[]}`,
		`{"covered":["package.json"],"findings":[{"path":"package.json","name":"react"}],"unsupported":[]}`,
		`{"covered":["package.json"],"findings":[],"unsupported":[],"ignored":true}`,
	} {
		if _, err := decodePeerResult([]byte(payload), []string{"package.json"}); err == nil {
			t.Fatalf("incomplete peer evidence accepted: %s", payload)
		}
	}
	result, err := decodePeerResult([]byte(`{"covered":["package.json"],"findings":[{"path":"package.json","name":"react","message":"version mismatch"}],"unsupported":[]}`), []string{"package.json"})
	if err != nil || len(result.Findings) != 1 {
		t.Fatalf("peer mismatch lost: %+v, %v", result, err)
	}
}
