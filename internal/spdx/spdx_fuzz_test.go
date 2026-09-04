package spdx

import "testing"

func FuzzExpressionBoundary(f *testing.F) {
	f.Add("MIT")
	f.Add("MIT OR Apache-2.0 AND BSD-3-Clause")
	f.Add("GPL-2.0-only+ WITH Classpath-exception-2.0")
	f.Add("(MIT")
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > maxExpressionBytes+1 {
			return
		}
		expression, err := Parse(value)
		if err == nil && expression.root == nil {
			t.Fatal("accepted expression has no syntax tree")
		}
	})
}
