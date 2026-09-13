package policy

import "testing"

func TestMatchUsesSegmentAwareWildcards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path, pattern string
		want          bool
	}{
		{"src/domain/file.ts", "src/domain/*", true},
		{"src/domain/nested/file.ts", "src/domain/*", false},
		{"src/domain/nested/file.ts", "src/domain/**", true},
		{"file.ts", "**/*.ts", true},
		{"deep/file.ts", "*.ts", false},
		{"deep/file.ts", "**/*.ts", true},
		{"src/deep/file.go", "src/**/file.go", true},
		{"src/file.go", "src/**/file.go", true},
		{"src/deep/file.go", "src/**/other.go", false},
		{"résumé.py", "résumé.?y", true},
	}
	for _, test := range cases {
		if got := Match(test.path, test.pattern); got != test.want {
			t.Errorf("Match(%q, %q) = %v, want %v", test.path, test.pattern, got, test.want)
		}
	}
}

func TestMatchKeepsRepeatedPolicyEvaluationBounded(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() {
		Match("src/application/handlers/example.go", "src/**/handlers/*.go")
	})
	if allocations > 8 {
		t.Fatalf("Match allocated %.0f objects per comparison", allocations)
	}
}

func TestPatternsOverlapRecognizesSharedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		left, right string
		want        bool
	}{
		{name: "nested data", left: "data/**/*.json", right: "data/*.json", want: true},
		{name: "separate extensions", left: "data/**/*.json", right: "data/**/*.yaml", want: false},
		{name: "control input", left: "**/*.json", right: "**/package.json", want: true},
		{name: "separate literals", left: "data/first.json", right: "data/second.json", want: false},
		{name: "directory star can be empty", left: ".github/workflows/**/*.yaml", right: ".github/workflows/check.yaml", want: true},
		{name: "nested control input", left: "data/**/*.json", right: "**/package.json", want: true},
		{name: "nested default exclude", left: "fixtures/node_modules/**/*.data.json", right: "**/node_modules/**", want: true},
		{name: "nested Python environment", left: "apps/api/.venv/**/*.py", right: "**/.venv/**", want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := PatternsOverlap(test.left, test.right); got != test.want {
				t.Fatalf("PatternsOverlap(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}
