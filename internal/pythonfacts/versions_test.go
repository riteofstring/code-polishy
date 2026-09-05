package pythonfacts

import (
	"testing"
)

func TestVersionFactsUsePEP440CanonicalEquality(t *testing.T) {
	inputs := []string{"1.0", "1.0.0", "01!2.0RC1", "1!2rc1", "1.0+LOCAL.1", "1+local.1", "1.0.post1", "1.0.dev1", "latest"}
	response, err := Analyze(typeTestInterpreter(t), Request{Versions: inputs})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]int{{0, 1}, {2, 3}, {4, 5}} {
		left, right := response.Versions[pair[0]], response.Versions[pair[1]]
		if left.Error != "" || right.Error != "" || left.Normalized == "" || left.Normalized != right.Normalized {
			t.Fatalf("equivalent versions did not match: %+v, %+v", left, right)
		}
	}
	if response.Versions[6].Normalized == response.Versions[7].Normalized || response.Versions[8].Error == "" {
		t.Fatalf("different or invalid versions lost identity: %+v", response.Versions)
	}
	response.Versions[0], response.Versions[1] = response.Versions[1], response.Versions[0]
	if err := validateResponse(response, normalizedRequest(Request{Versions: inputs})); err == nil {
		t.Fatal("reordered version evidence was accepted")
	}
}
