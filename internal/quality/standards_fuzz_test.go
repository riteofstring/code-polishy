package quality

import "testing"

func FuzzMarkdownAndShellSyntaxBoundaries(f *testing.F) {
	f.Add("# Heading\n\n[link](target.md)\n")
	f.Add("#!/usr/bin/env bash\nvalue=\"${HOME:-}\" # note\n")
	f.Add("```sh\nunterminated\n")
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 256*1024 {
			return
		}
		document := parseMarkdownDocument("fuzz.md", source)
		if document.path != "fuzz.md" || document.text != source {
			t.Fatalf("document identity changed")
		}
		annotations, err := scanShellSource([]byte(source))
		if err != nil {
			return
		}
		for _, annotation := range annotations {
			if annotation.offset < 0 || annotation.offset >= len(source) {
				t.Fatalf("annotation offset = %d for %d bytes", annotation.offset, len(source))
			}
		}
	})
}
