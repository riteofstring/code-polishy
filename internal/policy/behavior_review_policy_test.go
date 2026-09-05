package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBehaviorReviewPolicyOmissionKeepsReviewOptional(t *testing.T) {
	t.Parallel()
	config, err := Load(writeConfig(t, minimalConfig()), "")
	if err != nil {
		t.Fatal(err)
	}
	if config.Verification.BehaviorReview != nil {
		t.Fatalf("behavior review = %+v, want omitted", config.Verification.BehaviorReview)
	}
}

func TestBehaviorReviewPolicyDefaultsAndEffectiveFeatureRequirement(t *testing.T) {
	t.Parallel()
	config, err := Load(writeConfig(t, behaviorReviewConfig(`{"features":[{"name":"search","description":"Search query and result behavior.","paths":["web/search/**"],"suites":["content-test"]}]}`)), "")
	if err != nil {
		t.Fatal(err)
	}
	behaviorReview := config.Verification.BehaviorReview
	if behaviorReview == nil || behaviorReview.DefaultRequiredAt != BehaviorReviewOnRequest {
		t.Fatalf("behavior review = %+v", behaviorReview)
	}
	feature := behaviorReview.Features[0]
	if feature.RequiredAt != "" || behaviorReview.EffectiveRequiredAt(feature) != BehaviorReviewOnRequest {
		t.Fatalf("feature requirement = %q, effective = %q", feature.RequiredAt, behaviorReview.EffectiveRequiredAt(feature))
	}
}

func TestBehaviorReviewPolicyAcceptsConfiguredLevels(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		setting  string
		required string
	}{
		"on request default": {
			setting:  `{"defaultRequiredAt":"on-request","features":[{"name":"search","description":"Search query and result behavior.","modules":["content"],"suites":["content-test"]}]}`,
			required: BehaviorReviewOnRequest,
		},
		"merge default": {
			setting:  `{"defaultRequiredAt":"merge","features":[{"name":"search","description":"Search query and result behavior.","modules":["content"],"suites":["content-test"]}]}`,
			required: BehaviorReviewMerge,
		},
		"checkpoint feature": {
			setting:  `{"defaultRequiredAt":"merge","features":[{"name":"search","description":"Search query and result behavior.","modules":["content"],"suites":["content-test"],"requiredAt":"checkpoint"}]}`,
			required: BehaviorReviewCheckpoint,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config, err := Load(writeConfig(t, behaviorReviewConfig(testCase.setting)), "")
			if err != nil {
				t.Fatal(err)
			}
			behaviorReview := config.Verification.BehaviorReview
			if got := behaviorReview.EffectiveRequiredAt(behaviorReview.Features[0]); got != testCase.required {
				t.Fatalf("effective requirement = %q, want %q", got, testCase.required)
			}
		})
	}
}

func TestBehaviorReviewPolicyRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		config func() string
		want   string
	}{
		"unknown policy field": {
			config: func() string { return behaviorReviewConfig(`{"typo":true}`) },
			want:   schemaRejection,
		},
		"invalid default level": {
			config: func() string { return behaviorReviewConfig(`{"defaultRequiredAt":"always"}`) },
			want:   schemaRejection,
		},
		"duplicate names": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]},{"name":"checkout","description":"Checkout completion and payment behavior.","paths":["web/checkout/**"],"suites":["full"]}]}`)
			},
			want: "duplicate behavior review feature name",
		},
		"unknown module": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["missing"],"suites":["content-test"]}]}`)
			},
			want: "references unknown module",
		},
		"missing scope": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","suites":["content-test"]}]}`)
			},
			want: schemaRejection,
		},
		"invalid path": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","paths":["../checkout/**"],"suites":["content-test"]}]}`)
			},
			want: schemaRejection,
		},
		"empty suites": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":[]}]}`)
			},
			want: schemaRejection,
		},
		"unknown suite": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["missing"]}]}`)
			},
			want: "references unknown test suite",
		},
		"supplemental suite": {
			config: func() string {
				config := behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]}]}`)
				return strings.Replace(config, `"name":"content-test","kind":"content","scope":"module","modules":["content"],"argv":["go","test","./..."]`, `"name":"content-test","kind":"content","scope":"module","cost":"expensive","modules":["content"],"argv":["go","test","./..."],"runOn":["supplemental"]`, 1)
			},
			want: "references ineligible suite",
		},
		"mutation suite": {
			config: func() string {
				config := behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]}]}`)
				return strings.Replace(config, `"name":"content-test","kind":"content","scope":"module","modules":["content"],"argv":["go","test","./..."]`, `"name":"content-test","kind":"mutation","scope":"module","cost":"expensive","modules":["content"],"argv":["go","test","./..."],"runOn":["supplemental"]`, 1)
			},
			want: "references ineligible suite",
		},
		"credentialed suite": {
			config: func() string {
				config := behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]}]}`)
				return strings.Replace(config, `"name":"content-test","kind":"content"`, `"name":"content-test","kind":"credentialed"`, 1)
			},
			want: "references ineligible suite",
		},
		"destructive suite": {
			config: func() string {
				config := behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]}]}`)
				return strings.Replace(config, `"name":"content-test","kind":"content"`, `"name":"content-test","kind":"destructive"`, 1)
			},
			want: "references ineligible suite",
		},
		"declared environment": {
			config: func() string {
				config := behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"]}]}`)
				return strings.Replace(config, `"name":"content-test","kind":"content","scope":"module"`, `"name":"content-test","kind":"content","scope":"module","environment":["TOKEN"]`, 1)
			},
			want: "references ineligible suite",
		},
		"unknown feature field": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"],"typo":true}]}`)
			},
			want: schemaRejection,
		},
		"feature cannot explicitly use on request": {
			config: func() string {
				return behaviorReviewConfig(`{"features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"],"requiredAt":"on-request"}]}`)
			},
			want: schemaRejection,
		},
		"feature cannot weaken default": {
			config: func() string {
				return behaviorReviewConfig(`{"defaultRequiredAt":"checkpoint","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["content"],"suites":["content-test"],"requiredAt":"merge"}]}`)
			},
			want: "cannot weaken verification.behaviorReview.defaultRequiredAt",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(writeConfig(t, testCase.config()), "")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestBehaviorReviewSchemaMatchesPolicyContract(t *testing.T) {
	t.Parallel()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join(repositoryRoot, "schema", "code-polishy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Verification struct {
				Properties map[string]struct {
					Ref string `json:"$ref"`
				} `json:"properties"`
			} `json:"verification"`
			BehaviorReview struct {
				AdditionalProperties *bool `json:"additionalProperties"`
				Properties           map[string]struct {
					Default string   `json:"default"`
					Enum    []string `json:"enum"`
				} `json:"properties"`
			} `json:"behaviorReview"`
			BehaviorReviewFeature struct {
				AdditionalProperties *bool    `json:"additionalProperties"`
				Required             []string `json:"required"`
				Properties           map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"behaviorReviewFeature"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Defs.Verification.Properties["behaviorReview"].Ref != "#/$defs/behaviorReview" {
		t.Fatalf("behavior review schema reference = %q", schema.Defs.Verification.Properties["behaviorReview"].Ref)
	}
	behaviorReview := schema.Defs.BehaviorReview
	if behaviorReview.AdditionalProperties == nil || *behaviorReview.AdditionalProperties ||
		!slices.Equal(behaviorReview.Properties["defaultRequiredAt"].Enum, []string{BehaviorReviewOnRequest, BehaviorReviewMerge, BehaviorReviewCheckpoint}) ||
		behaviorReview.Properties["defaultRequiredAt"].Default != BehaviorReviewOnRequest {
		t.Fatalf("behavior review schema = %+v", behaviorReview)
	}
	feature := schema.Defs.BehaviorReviewFeature
	if feature.AdditionalProperties == nil || *feature.AdditionalProperties ||
		!slices.Equal(feature.Required, []string{"name", "description", "suites"}) ||
		!slices.Equal(feature.Properties["requiredAt"].Enum, []string{BehaviorReviewMerge, BehaviorReviewCheckpoint}) {
		t.Fatalf("behavior review feature schema = %+v", feature)
	}
}

func behaviorReviewConfig(setting string) string {
	return strings.Replace(minimalConfig(), `"checks":[]`, `"verification":{"behaviorReview":`+setting+`},"checks":[]`, 1)
}
