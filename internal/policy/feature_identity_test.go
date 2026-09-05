package policy

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestFeatureAliasesResolveExplicitUnicodeIdentities(t *testing.T) {
	t.Parallel()
	checkout := aliasTestFeature("checkout", " Purchase\u00a0Completion ", "Straße", "Café")
	search := aliasTestFeature("search", "ﬁnd\u2003results")
	config, err := Parse([]byte(aliasTestConfiguration(t, checkout, search)), ConfigFilename)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.Verification.BehaviorReview.Features[0].Aliases, checkout.Aliases) {
		t.Fatal("configuration did not preserve declared alias spelling")
	}
	selected, err := ResolveBehaviorReviewFeatures(config, []string{
		"ＣＨＥＣＫＯＵＴ", "purchase\t\ncompletion", "STRASSE", "cafe\u0301", "FIND RESULTS", "checkout",
	})
	if err != nil || !slices.Equal(selected, []string{"checkout", "search"}) {
		t.Fatalf("selected = %v, error = %v", selected, err)
	}
	for _, operand := range []string{"purchase", "complete checkout", "Published behavior.", "search results"} {
		if selected, err := ResolveBehaviorReviewFeatures(config, []string{operand}); err == nil || len(selected) != 0 {
			t.Fatalf("inexact operand %q selected %v: %v", operand, selected, err)
		}
	}
	if selected, err := ResolveBehaviorReviewFeatures(config, nil); err != nil || len(selected) != 0 {
		t.Fatalf("no explicit operands selected %v: %v", selected, err)
	}
}

func TestFeatureAliasesRejectAmbiguousDeclarations(t *testing.T) {
	t.Parallel()
	cases := map[string][]BehaviorReviewFeature{
		"canonical alias": {aliasTestFeature("checkout"), aliasTestFeature("search", "ＣＨＥＣＫＯＵＴ")},
		"same feature":    {aliasTestFeature("checkout", "CHECKOUT")},
		"case fold":       {aliasTestFeature("checkout", "Straße"), aliasTestFeature("search", "STRASSE")},
		"composition":     {aliasTestFeature("checkout", "Café"), aliasTestFeature("search", "CAFE\u0301")},
		"whitespace":      {aliasTestFeature("checkout", "purchase\u00a0completion"), aliasTestFeature("search", " PURCHASE\tCOMPLETION ")},
		"compatibility":   {aliasTestFeature("search", "ﬁnd", "FIND")},
	}
	for name, features := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(aliasTestConfiguration(t, features...)), ConfigFilename)
			if err == nil || !strings.Contains(err.Error(), "collides") {
				t.Fatalf("error = %v, want ambiguous feature identity", err)
			}
		})
	}
}

func TestBehaviorFeatureDescriptionsAndAliasesAreBounded(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*BehaviorReviewFeature){
		"empty description":          func(f *BehaviorReviewFeature) { f.Description = "" },
		"untrimmed description":      func(f *BehaviorReviewFeature) { f.Description = " Published behavior." },
		"blank description":          func(f *BehaviorReviewFeature) { f.Description = "\u2003" },
		"long description":           func(f *BehaviorReviewFeature) { f.Description = strings.Repeat("x", 513) },
		"description byte bound":     func(f *BehaviorReviewFeature) { f.Description = strings.Repeat("é", 257) },
		"description control":        func(f *BehaviorReviewFeature) { f.Description = "Published\nbehavior." },
		"empty alias":                func(f *BehaviorReviewFeature) { f.Aliases = []string{""} },
		"blank alias":                func(f *BehaviorReviewFeature) { f.Aliases = []string{"\u00a0\t"} },
		"long alias":                 func(f *BehaviorReviewFeature) { f.Aliases = []string{strings.Repeat("x", 257)} },
		"alias byte bound":           func(f *BehaviorReviewFeature) { f.Aliases = []string{strings.Repeat("é", 129)} },
		"normalized alias expansion": func(f *BehaviorReviewFeature) { f.Aliases = []string{strings.Repeat("ﷺ", 30)} },
		"alias control":              func(f *BehaviorReviewFeature) { f.Aliases = []string{"search\x00"} },
		"alias hidden formatting":    func(f *BehaviorReviewFeature) { f.Aliases = []string{"sea\u200brch"} },
		"duplicate alias":            func(f *BehaviorReviewFeature) { f.Aliases = []string{"find", "find"} },
		"too many aliases": func(f *BehaviorReviewFeature) {
			for _, alias := range "abcdefghijklmnopq" {
				f.Aliases = append(f.Aliases, string(alias))
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			feature := aliasTestFeature("search")
			mutate(&feature)
			if _, err := Parse([]byte(aliasTestConfiguration(t, feature)), ConfigFilename); err == nil {
				t.Fatal("accepted malformed feature declaration")
			}
		})
	}
	missingDescription := behaviorReviewConfig(`{"features":[{"name":"search","modules":["content"],"suites":["content-test"]}]}`)
	if _, err := Parse([]byte(missingDescription), ConfigFilename); err == nil {
		t.Fatal("accepted a feature without a description")
	}
	if _, err := NormalizeFeatureAlias(string([]byte{0xff})); err == nil {
		t.Fatal("accepted a non-UTF-8 feature operand")
	}
	features := make([]BehaviorReviewFeature, 129)
	for index := range features {
		features[index] = aliasTestFeature("feature-" + strconv.Itoa(index))
	}
	if _, err := Parse([]byte(aliasTestConfiguration(t, features[:128]...)), ConfigFilename); err != nil {
		t.Fatalf("valid bounded inventory: %v", err)
	}
	if _, err := Parse([]byte(aliasTestConfiguration(t, features...)), ConfigFilename); err == nil || !strings.Contains(err.Error(), "maxItems") {
		t.Fatal("accepted an oversized feature inventory")
	}
	if _, err := ResolveBehaviorReviewFeatures(Config{}, make([]string, 129)); err == nil {
		t.Fatal("accepted an oversized explicit selection")
	}
}

func aliasTestFeature(name string, aliases ...string) BehaviorReviewFeature {
	return BehaviorReviewFeature{
		Name: name, Description: "Published behavior.", Aliases: aliases,
		Modules: []string{"content"}, Suites: []string{"content-test"},
	}
}

func aliasTestConfiguration(t testing.TB, features ...BehaviorReviewFeature) string {
	t.Helper()
	data, err := json.Marshal(BehaviorReviewPolicy{DefaultRequiredAt: BehaviorReviewOnRequest, Features: features})
	if err != nil {
		t.Fatal(err)
	}
	return behaviorReviewConfig(string(data))
}
