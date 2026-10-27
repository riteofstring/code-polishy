package quality

import (
	"fmt"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestSourceCommentAllowsLicenseNoticeHeaders(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{"go", "sample/main.go", "// SPDX-License-Identifier: Apache-2.0\n// SPDX-FileCopyrightText: 2019 Jane Doe\n\n//go:build linux\n\npackage sample\n"},
		{"python-after-shebang", "sample/main.py", "#!/usr/bin/env python3\n# coding: utf-8\n# SPDX-FileCopyrightText: Copyright 2019-2024 The Authors\n# SPDX-License-Identifier: MIT OR BSD-3-Clause\nvalue = 1\n"},
		{"python-crlf", "sample/crlf.py", "# SPDX-License-Identifier: BSD-3-Clause\r\n# SPDX-FileCopyrightText: (c) 2020 Jane Doe\r\nvalue = 1\r\n"},
		{"shell", "sample/main.sh", "#!/usr/bin/env bash\n\n# SPDX-License-Identifier: LicenseRef-Internal\nprintf '%s\\n' value\n"},
		{"powershell", "sample/main.ps1", "# SPDX-FileCopyrightText: © 2021 Jane Doe\n$value = 1\n"},
		{"css", "sample/main.css", "/* SPDX-License-Identifier: Apache-2.0 WITH LLVM-exception */\n.sample { color: red; }\n"},
		{"html", "sample/main.html", "<!-- SPDX-License-Identifier: MIT -->\n<!doctype html>\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if findings := sourceCommentFindingsFor(t, testCase.path, testCase.source); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentRejectsLicenseNoticeNearMisses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		path     string
		source   string
		subjects []string
	}{
		{"after-code", "sample/main.py", "value = 1\n# SPDX-License-Identifier: MIT\n", []string{"2:1"}},
		{"after-prose", "sample/main.py", "# prose\n# SPDX-License-Identifier: MIT\nvalue = 1\n", []string{"1:1"}},
		{"after-docstring", "sample/main.py", "\"\"\"Module.\"\"\"\n# SPDX-License-Identifier: MIT\nvalue = 1\n", []string{"1:1", "2:1"}},
		{"go-after-package", "sample/main.go", "package sample\n// SPDX-License-Identifier: MIT\n", []string{"2:1"}},
		{"indented", "sample/main.sh", "  # SPDX-License-Identifier: MIT\nprintf value\n", []string{"1:3"}},
		{"unknown-license", "sample/main.py", "# SPDX-License-Identifier: Not-A-License\nvalue = 1\n", []string{"1:1"}},
		{"trailing-prose", "sample/main.py", "# SPDX-License-Identifier: MIT because upstream\nvalue = 1\n", []string{"1:1"}},
		{"tag-spacing", "sample/main.py", "#  SPDX-License-Identifier: MIT\nvalue = 1\n", []string{"1:1"}},
		{"unknown-tag", "sample/main.py", "# SPDX-FileComment: explains the module\nvalue = 1\n", []string{"1:1"}},
		{"copyright-prose", "sample/main.py", "# SPDX-FileCopyrightText: This module parses requests\nvalue = 1\n", []string{"1:1"}},
		{"copyright-empty", "sample/main.py", "# SPDX-FileCopyrightText: \nvalue = 1\n", []string{"1:1"}},
		{"css-multiline", "sample/main.css", "/* SPDX-License-Identifier: MIT\n */\n.sample { color: red; }\n", []string{"1:1"}},
		{"html-delimiter", "sample/main.html", "<!--SPDX-License-Identifier: MIT-->\n", []string{"1:1"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := sourceCommentFindingsFor(t, testCase.path, testCase.source)
			subjects := sourceCommentSubjects(findings)
			slices.Sort(subjects)
			if !slices.Equal(subjects, testCase.subjects) {
				t.Fatalf("subjects = %v, findings = %+v", subjects, findings)
			}
		})
	}
}

func TestJavaScriptLintAllowsOnlyLicenseNoticeHeaders(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	allowed := []javascript.LintComment{
		{Path: "src/main.ts", Kind: "Line", Raw: "// SPDX-License-Identifier: Apache-2.0", Complete: true, Line: 1, Column: 1, BeforeCode: true, Preamble: true},
		{Path: "src/main.ts", Kind: "Line", Raw: "// SPDX-FileCopyrightText: 2024 Jane Doe", Complete: true, Line: 2, Column: 1, BeforeCode: true},
	}
	if findings := javascriptLintResultFindings(repo, javascript.LintResult{Comments: allowed}); len(findings) != 0 {
		t.Fatalf("allowed license notices = %+v", findings)
	}
	rejected := []javascript.LintComment{
		{Path: "src/main.ts", Kind: "Line", Raw: "// SPDX-License-Identifier: MIT", Complete: true, Line: 4, Column: 1},
		{Path: "src/main.ts", Kind: "Line", Raw: "// SPDX-License-Identifier: MIT", Complete: true, Line: 1, Column: 3, BeforeCode: true},
		{Path: "src/main.ts", Kind: "Block", Raw: "/* SPDX-License-Identifier: MIT */", Complete: true, Line: 1, Column: 1, BeforeCode: true},
		{Path: "src/main.ts", Kind: "Line", Raw: "// SPDX-License-Identifier: MIT", Line: 1, Column: 1, BeforeCode: true},
	}
	findings := javascriptLintResultFindings(repo, javascript.LintResult{Comments: rejected})
	if len(findings) != len(rejected) {
		t.Fatalf("rejected license notices = %+v", findings)
	}
	for index, finding := range findings {
		if finding.Check != "policy.sourceComment" || finding.Subject != fmt.Sprintf("%d:%d", rejected[index].Line, rejected[index].Column) {
			t.Fatalf("finding %d = %+v", index, finding)
		}
	}
}

func TestRustSourcePolicyAllowsOnlyLicenseNoticeHeaders(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	denied := false
	repo.Config.Quality.AllowComments = &denied
	writeQualityFile(t, repo.Root, "src/lib.rs", "// SPDX-License-Identifier: MIT OR Apache-2.0\n// SPDX-FileCopyrightText: 2024 Jane Doe\n\npub fn value() -> u8 { 1 }\n// SPDX-License-Identifier: MIT\n")
	findings := rustSourceFindings(t.Context(), repo, []string{"src/lib.rs"}, runner.OSRunner{})
	if len(findings) != 1 || findings[0].Check != "policy.sourceComment" || findings[0].Line != 5 {
		t.Fatalf("expected only the notice after code: %+v", findings)
	}
}
