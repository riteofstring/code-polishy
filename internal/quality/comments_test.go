package quality

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestSourceCommentFindingsRejectRealAnnotations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		source  string
		subject string
	}{
		{"go", "sample/main.go", "package sample\n// prose\nvar value = 1\n", "2:1"},
		{"python", "sample/main.py", "# prose\nvalue = 1\n", "1:1"},
		{"shell", "sample/main.sh", "# prose\nprintf '%s\\n' value\n", "1:1"},
		{"css", "sample/main.css", ".sample { /* prose */ color: red; }\n", "1:11"},
		{"html", "sample/main.html", "<!doctype html>\n<!-- prose -->\n", "2:1"},
		{"html-bogus", "sample/bogus.html", "<! prose>\n", "1:1"},
		{"html-processing", "sample/processing.html", "<? prose>\n", "1:1"},
		{"powershell-line", "sample/main.ps1", "# prose\n$value = 1\n", "1:1"},
		{"powershell-block", "sample/block.ps1", "<# prose #>\n$value = 1\n", "1:1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := sourceCommentFindingsFor(t, testCase.path, testCase.source)
			if len(findings) != 1 || findings[0].Check != "policy.sourceComment" || findings[0].Subject != testCase.subject {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestAllowCommentsDisablesOnlySourceCommentFindings(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, "sample/main.py", "# prose\nvalue = 1  \n")
	strict := findingChecks(sourceChecks(repo, []string{"sample/main.py"}))
	if !strict["policy.sourceComment"] || !strict["quality.trailingWhitespace"] {
		t.Fatalf("strict findings = %+v", strict)
	}
	allowComments := true
	repo.Config.Quality.AllowComments = &allowComments
	permissive := findingChecks(sourceChecks(repo, []string{"sample/main.py"}))
	if permissive["policy.sourceComment"] || !permissive["quality.trailingWhitespace"] {
		t.Fatalf("allowComments findings = %+v", permissive)
	}
}

func TestSourceCommentFindingsIgnoreLiteralText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{"go", "sample/main.go", "package sample\nvar value = \"// prose\"\n"},
		{"python", "sample/main.py", "value = '# prose'\nrendered = f'{value:#x}'\n"},
		{"shell-string", "sample/main.sh", "printf '%s\\n' '# prose'\n"},
		{"shell-heredoc", "sample/heredoc.sh", "cat <<'EOF'\n# prose\n$(printf '# prose')\nEOF\n"},
		{"shell-unquoted-data", "sample/unquoted.sh", "cat <<EOF\n# prose\nEOF\n"},
		{"css", "sample/main.css", ".sample { content: \"/* prose */\"; value: \\/*; }\n"},
		{"html-attribute", "sample/attribute.html", "<div title=\"<!-- prose -->\"></div>\n"},
		{"html-raw", "sample/raw.html", "<textarea><!-- prose --></textarea>\n<script src=\"main.js\"></script>\n"},
		{"powershell-string", "sample/main.ps1", "$value = \"# prose\"\n$single = '# prose'\n"},
		{"powershell-here", "sample/here.ps1", "$single = @'\n# prose\n'@\n$double = @\"\n# prose $name\n\"@\n"},
		{"powershell-current-interpolation", "sample/interpolation.ps1", "$release = \"$($Manifest.codePolishyVersion)-$($Manifest.releaseDigest)\"\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if findings := sourceCommentFindingsFor(t, testCase.path, testCase.source); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentScannersHonorGrammarBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		path      string
		source    string
		wantCheck string
		wantCount int
	}{
		{name: "css-empty-comment", path: "sample/main.css", source: "/**/\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "css-lone-slash", path: "sample/main.css", source: "/\n"},
		{name: "css-terminal-escape", path: "sample/main.css", source: `\`, wantCheck: "policy.sourceCommentCoverage", wantCount: 1},
		{name: "css-escaped-quotes", path: "sample/main.css", source: `.a { first: "a\"/* literal */"; second: 'a\'/* literal */'; }` + "\n"},
		{name: "html-minimum-comment", path: "sample/main.html", source: "<!---->\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "html-doctype-whitespace", path: "sample/main.html", source: "<!doctype html>\n<!DOCTYPE\thtml>\n<!doctype\fhtml>\n"},
		{name: "html-doctype-near-miss", path: "sample/main.html", source: "<!doctypehtml>\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "html-name-boundaries", path: "sample/main.html", source: `<A0:_- title="<!-- literal -->"></A0:_-><Z title="<!-- literal -->"></Z><a title="<!-- literal -->"></a><z9 title="<!-- literal -->"></z9>` + "\n"},
		{name: "html-raw-elements", path: "sample/main.html", source: `<title><!-- literal --></title><xmp><!-- literal --></xmp><iframe><!-- literal --></iframe><noembed><!-- literal --></noembed><noframes><!-- literal --></noframes><plaintext><!-- literal -->` + "\n"},
		{name: "python-bom-encoding", path: "sample/main.py", source: "\xef\xbb\xbf# coding: utf-8\nvalue = 1\n"},
		{name: "python-identifier-and-continuation", path: "sample/main.py", source: "A0_ = 1; Z9 = 2; a = 3; z = 4 \\\n    + 5 # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "python-escaped-f-string-braces", path: "sample/main.py", source: `value = f"{{literal}}"` + "\n"},
		{name: "python-unescaped-f-string-brace", path: "sample/main.py", source: `value = f"}"` + "\n", wantCheck: "policy.sourceCommentCoverage", wantCount: 1},
		{name: "python-inline-docstring", path: "sample/main.py", source: `def A0_(): "documentation"` + "\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "shell-backtick", path: "sample/main.sh", source: "value=`printf '%s' '# literal'` # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "shell-ansi-quote", path: "sample/main.sh", source: "value=$'# literal' # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "shell-process-substitution", path: "sample/main.sh", source: "cat <(printf '%s' '# literal') # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "shell-parameter-expansion", path: "sample/main.sh", source: "value=${value:-'# literal'} # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "shell-tabbed-heredoc", path: "sample/main.sh", source: "cat <<-EOF\n\t# literal\n\tEOF\n# prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "powershell-terminal-escape", path: "sample/main.ps1", source: "`", wantCheck: "policy.sourceCommentCoverage", wantCount: 1},
		{name: "powershell-doubled-single-quote", path: "sample/main.ps1", source: "$value = 'a''# literal' # prose\n", wantCheck: "policy.sourceComment", wantCount: 1},
		{name: "powershell-index-zero-here-string", path: "sample/main.ps1", source: "@'\n# literal\n'@\n"},
		{name: "powershell-crlf-here-string", path: "sample/main.ps1", source: "$value=@\"\r\n# literal\r\n\"@\r\n"},
		{name: "powershell-nested-block-comment", path: "sample/main.ps1", source: "<# outer <# inner #> outer #>\n", wantCheck: "policy.sourceComment", wantCount: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := sourceCommentFindingsFor(t, testCase.path, testCase.source)
			if len(findings) != testCase.wantCount {
				t.Fatalf("findings = %+v", findings)
			}
			for _, finding := range findings {
				if finding.Check != testCase.wantCheck {
					t.Fatalf("finding = %+v", finding)
				}
			}
		})
	}
}

func TestPythonDocstringsAreSourceComments(t *testing.T) {
	t.Parallel()
	findings := sourceCommentFindingsFor(t, "sample/main.py", "\"\"\"module\"\"\"\nclass Widget:\n    \"\"\"class\"\"\"\n    def run(self):\n        \"\"\"function\"\"\"\n        return 1\n")
	if len(findings) != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	if subjects := sourceCommentSubjects(findings); !slices.Equal(subjects, []string{"1:1", "3:5", "5:9"}) {
		t.Fatalf("subjects = %v", subjects)
	}
	if findings := sourceCommentFindingsFor(t, "sample/not-docstrings.py", "value = \"module\"\ndef run():\n    value = \"function\"\n    return value\n"); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSourceCommentAllowedMachineInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
		target string
	}{
		{"go-build", "sample/build.go", "//go:build linux\n\npackage sample\n", ""},
		{"go-generate", "sample/generate.go", "package sample\n//go:generate go run tool.go\n", ""},
		{"go-line", "sample/line.go", "package sample\n//line generated.go:12:3\nvar value = 1\n", ""},
		{"go-embed", "sample/embed.go", "package sample\n//go:embed asset.txt\nvar asset string\n", ""},
		{"go-function", "sample/function.go", "package sample\n//go:noinline\nfunc value() {}\n", ""},
		{"go-export", "sample/export.go", "package sample\nimport \"C\"\n//export Value\nfunc Value() {}\n", ""},
		{"go-debug", "sample/debug.go", "//go:debug asynctimerchan=1\npackage main\n", ""},
		{"go-cgo", "sample/cgo.go", "package sample\n/*\n#cgo CFLAGS: -DVALUE\n*/\nimport \"C\"\n", ""},
		{"python-line-one", "sample/encoding.py", "# coding: utf-8\nvalue = 1\n", ""},
		{"python-line-two", "sample/encoding.py", "\n# -*- coding: utf-8 -*-\nvalue = 1\n", ""},
		{"python-shebang", "sample/shebang.py", "#!/usr/bin/env python3\n# coding=utf-8\nvalue = 1\n", ""},
		{"shell-shebang", "sample/main.sh", "#!/usr/bin/env bash\nprintf '%s\\n' value\n", ""},
		{"shell-sbatch", "sample/job.sbatch", "#!/usr/bin/env bash\n\n#SBATCH --job-name=sample\n#SBATCH --time=00:05:00\nprintf '%s\\n' value\n", ""},
		{"shell-source-dynamic", "sample/main.sh", "# shellcheck source=lib/source.sh\nsource \"${policy_root}/lib/source.sh\"\n", "lib/source.sh"},
		{"shell-dot-dynamic", "sample/dot.sh", "# shellcheck source=lib/source.sh\n. \"$(pwd)/lib/source.sh\"\n", "lib/source.sh"},
		{"powershell-shebang", "sample/main.ps1", "#!pwsh\n$value = 1\n", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := qualityRepository(t)
			writeQualityFile(t, repo.Root, testCase.path, testCase.source)
			if testCase.target != "" {
				writeQualityFile(t, repo.Root, testCase.target, "printf '%s\\n' source\n")
			}
			if findings := sourceCommentFindings(repo, []string{testCase.path}); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentAllowsEveryGoDirective(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{"build", "sample/build.go", "//go:build linux && amd64\n\npackage sample\n"},
		{"generate", "sample/generate.go", "package sample\n//go:generate go run tool.go\n"},
		{"line", "sample/line.go", "package sample\n//line generated.go:12:3\nvar value = 1\n"},
		{"debug", "sample/debug.go", "//go:debug asynctimerchan=1\npackage main\n"},
		{"embed", "sample/embed.go", "package sample\n//go:embed asset.txt \"asset file.txt\" `assets`\nvar assets string\n"},
		{"linkname-function", "sample/linkname.go", "package sample\nimport _ \"unsafe\"\n//go:linkname local remote\nfunc local() {}\n"},
		{"linkname-variable", "sample/linkname.go", "package sample\nimport _ \"unsafe\"\n//go:linkname local remote.target\nvar local int\n"},
		{"wasmimport", "sample/import.go", "package sample\n//go:wasmimport module name\nfunc imported()\n"},
		{"wasmexport", "sample/export.go", "package sample\n//go:wasmexport exported\nfunc exported() {}\n"},
		{"export", "sample/cgo_export.go", "package sample\nimport \"C\"\n//export Value\nfunc Value() {}\n"},
		{"cgo", "sample/cgo.go", "package sample\n/*\n#cgo CFLAGS: -DVALUE\n*/\nimport \"C\"\n"},
		{"nointerface", "sample/nointerface.go", "package sample\n//go:nointerface\nfunc value() {}\n"},
		{"noescape", "sample/noescape.go", "package sample\n//go:noescape\nfunc value()\n"},
		{"nosplit", "sample/nosplit.go", "package sample\n//go:nosplit\nfunc value() {}\n"},
		{"noinline", "sample/noinline.go", "package sample\n//go:noinline\nfunc value() {}\n"},
		{"norace", "sample/norace.go", "package sample\n//go:norace\nfunc value() {}\n"},
		{"nocheckptr", "sample/nocheckptr.go", "package sample\n//go:nocheckptr\nfunc value() {}\n"},
		{"nowritebarrier", "sample/nowritebarrier.go", "package sample\n//go:nowritebarrier\nfunc value() {}\n"},
		{"nowritebarrierrec", "sample/nowritebarrierrec.go", "package sample\n//go:nowritebarrierrec\nfunc value() {}\n"},
		{"yeswritebarrierrec", "sample/yeswritebarrierrec.go", "package sample\n//go:yeswritebarrierrec\nfunc value() {}\n"},
		{"systemstack", "sample/systemstack.go", "package sample\n//go:systemstack\nfunc value() {}\n"},
		{"cgo-unsafe-args", "sample/cgo_unsafe_args.go", "package sample\n//go:cgo_unsafe_args\nfunc value() {}\n"},
		{"uintptrescapes", "sample/uintptrescapes.go", "package sample\n//go:uintptrescapes\nfunc value() {}\n"},
		{"uintptrkeepalive", "sample/uintptrkeepalive.go", "package sample\n//go:uintptrkeepalive\nfunc value() {}\n"},
		{"registerparams", "sample/registerparams.go", "package sample\n//go:registerparams\nfunc value() {}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if findings := sourceCommentFindingsFor(t, testCase.path, testCase.source); len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestGoDirectiveGrammarBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		source  string
		allowed bool
		check   string
	}{
		{name: "line-low-digit", path: "sample/line.go", source: "package sample\n//line a.go:1\nvar value = 1\n", allowed: true},
		{name: "line-high-digit", path: "sample/line.go", source: "package sample\n//line z.go:9:9\nvar value = 1\n", allowed: true},
		{name: "line-zero", path: "sample/line.go", source: "package sample\n//line a.go:0\nvar value = 1\n", check: "policy.sourceCommentCoverage"},
		{name: "line-zero-column", path: "sample/line.go", source: "package sample\n//line a.go:1:0\nvar value = 1\n", check: "policy.sourceCommentCoverage"},
		{name: "line-nondigit", path: "sample/line.go", source: "package sample\n//line a.go:1:a\nvar value = 1\n", check: "policy.sourceCommentCoverage"},
		{name: "build-crlf-gap", path: "sample/build.go", source: "//go:build linux\r\n\t\r\npackage sample\r\n", allowed: true},
		{name: "debug-test-package", path: "sample/debug_test.go", source: "//go:debug asynctimerchan=1\npackage sample\n", allowed: true},
		{name: "embed-single-bare", path: "sample/embed.go", source: "package sample\n//go:embed a\nvar value string\n", allowed: true},
		{name: "embed-single-quoted", path: "sample/embed.go", source: "package sample\n//go:embed \"z file\"\nvar value string\n", allowed: true},
		{name: "embed-single-raw", path: "sample/embed.go", source: "package sample\n//go:embed `all:a/z`\nvar value string\n", allowed: true},
		{name: "embed-empty-quoted", path: "sample/embed.go", source: "package sample\n//go:embed \"\"\nvar value string\n"},
		{name: "embed-empty-raw", path: "sample/embed.go", source: "package sample\n//go:embed ``\nvar value string\n"},
		{name: "embed-dot-segment", path: "sample/embed.go", source: "package sample\n//go:embed a/./z\nvar value string\n"},
		{name: "embed-parent-segment", path: "sample/embed.go", source: "package sample\n//go:embed a/../z\nvar value string\n"},
		{name: "embed-double-slash", path: "sample/embed.go", source: "package sample\n//go:embed a//z\nvar value string\n"},
		{name: "embed-trailing-slash", path: "sample/embed.go", source: "package sample\n//go:embed a/\nvar value string\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := sourceCommentFindingsFor(t, testCase.path, testCase.source)
			if testCase.allowed && len(findings) != 0 {
				t.Fatalf("findings = %+v", findings)
			}
			check := testCase.check
			if check == "" {
				check = "policy.sourceComment"
			}
			if !testCase.allowed && (len(findings) != 1 || findings[0].Check != check) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentDirectiveNearMisses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
		target string
	}{
		{"go-build-placement", "sample/build.go", "//go:build linux\npackage sample\n", ""},
		{"go-build-trailing", "sample/build.go", "//go:build linux prose\n\npackage sample\n", ""},
		{"go-generate-indent", "sample/generate.go", "package sample\n //go:generate echo value\n", ""},
		{"go-generate-trailing", "sample/generate.go", "package sample\n//go:generate echo value \n", ""},
		{"go-line-column", "sample/line.go", "package sample\n //line generated.go:12\nvar value = 1\n", ""},
		{"go-line-filename", "sample/line.go", "package sample\n//line generated:other.go:12\nvar value = 1\n", ""},
		{"go-debug-trailing", "sample/debug.go", "//go:debug asynctimerchan=1 prose\npackage main\n", ""},
		{"go-function-intervening", "sample/function.go", "package sample\n//go:noinline\nconst value = 1\nfunc run() {}\n", ""},
		{"go-function-separated", "sample/function.go", "package sample\n//go:noinline\n\nfunc run() {}\n", ""},
		{"go-function-trailing", "sample/function.go", "package sample\n//go:noinline prose\nfunc run() {}\n", ""},
		{"go-noescape-body", "sample/function.go", "package sample\n//go:noescape\nfunc run() {}\n", ""},
		{"go-embed-target", "sample/embed.go", "package sample\n//go:embed asset.txt\nconst asset = \"asset\"\n", ""},
		{"go-embed-grammar", "sample/embed.go", "package sample\n//go:embed /asset.txt\nvar asset string\n", ""},
		{"go-linkname-fields", "sample/linkname.go", "package sample\n//go:linkname local remote extra\nfunc local() {}\n", ""},
		{"go-wasmimport-fields", "sample/import.go", "package sample\n//go:wasmimport module name prose\nfunc imported()\n", ""},
		{"go-wasmimport-body", "sample/import.go", "package sample\n//go:wasmimport module name\nfunc imported() {}\n", ""},
		{"go-wasmexport-prototype", "sample/export.go", "package sample\n//go:wasmexport exported\nfunc exported()\n", ""},
		{"go-export-cgo", "sample/export.go", "package sample\n//export Value\nfunc Value() {}\n", ""},
		{"go-export-trailing", "sample/export.go", "package sample\nimport \"C\"\n//export Value prose\nfunc Value() {}\n", ""},
		{"go-legacy-build", "sample/build.go", "// +build linux\n\npackage sample\n", ""},
		{"go-binary-only", "sample/build.go", "//go:binary-only-package\n\npackage sample\n", ""},
		{"go-cgo-stranded", "sample/cgo.go", "package sample\n// #cgo CFLAGS: -DVALUE\nvar value = 1\n", ""},
		{"go-cgo-separated", "sample/cgo.go", "package sample\n/* #cgo CFLAGS: -DVALUE */\n\nimport \"C\"\n", ""},
		{"python-third-line", "sample/encoding.py", "\n\n# coding: utf-8\nvalue = 1\n", ""},
		{"python-malformed", "sample/encoding.py", "# coding: utf 8\nvalue = 1\n", ""},
		{"shell-source-target", "sample/main.sh", "# shellcheck source=lib/source.css\nsource \"${policy_root}/lib/source.css\"\n", "lib/source.css"},
		{"shell-source-next-line", "sample/main.sh", "# shellcheck source=lib/source.sh\nprintf '%s\\n' value\nsource lib/source.sh\n", "lib/source.sh"},
		{"shell-source-operand", "sample/main.sh", "# shellcheck source=lib/source.sh\nsource\n", "lib/source.sh"},
		{"shell-sbatch-indented", "sample/job.sbatch", "#!/usr/bin/env bash\n #SBATCH --time=00:05:00\nprintf value\n", ""},
		{"shell-sbatch-after-code", "sample/job.sbatch", "#!/usr/bin/env bash\nprintf value\n#SBATCH --time=00:05:00\n", ""},
		{"shell-sbatch-empty", "sample/job.sbatch", "#!/usr/bin/env bash\n#SBATCH   \nprintf value\n", ""},
		{"shell-sbatch-glued", "sample/job.sbatch", "#!/usr/bin/env bash\n#SBATCH--time=00:05:00\nprintf value\n", ""},
		{"shell-sbatch-ordinary-comment", "sample/job.sbatch", "#!/usr/bin/env bash\n#SBATCH --time=00:05:00\n# prose\nprintf value\n", ""},
		{"powershell-shebang-position", "sample/main.ps1", "$value = 1\n#!pwsh\n", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := qualityRepository(t)
			writeQualityFile(t, repo.Root, testCase.path, testCase.source)
			if testCase.target != "" {
				writeQualityFile(t, repo.Root, testCase.target, "value\n")
			}
			findings := sourceCommentFindings(repo, []string{testCase.path})
			if len(findings) != 1 || findings[0].Check != "policy.sourceComment" {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentMalformedSourceFailsCoverage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{"go", "sample/main.go", "package\n"},
		{"python-string", "sample/main.py", "value = \"unfinished\n"},
		{"python-f-string", "sample/main.py", "value = f\"{value\"\n"},
		{"shell-string", "sample/main.sh", "printf '%s\n"},
		{"shell-substitution", "sample/main.sh", "value=$(printf value\n"},
		{"shell-heredoc", "sample/main.sh", "cat <<EOF\nvalue\n"},
		{"css-comment", "sample/main.css", ".sample { /* unfinished\n"},
		{"css-string", "sample/main.css", ".sample { content: \"unfinished; }\n"},
		{"html-comment", "sample/main.html", "<!-- unfinished\n"},
		{"html-attribute", "sample/main.html", "<div title=\"unfinished>\n"},
		{"html-script", "sample/main.html", "<script>const value = 1;</script>\n"},
		{"powershell-string", "sample/main.ps1", "$value = \"unfinished\n"},
		{"powershell-header", "sample/main.ps1", "$value = @\"unfinished\"\n"},
		{"powershell-close", "sample/main.ps1", "$value = @\"\ntext\n\"@ trailing\n"},
		{"powershell-block", "sample/main.ps1", "<# unfinished\n"},
		{"powershell-interpolation", "sample/main.ps1", "$value = \"$(Get-Value # prose)\"\n"},
		{"powershell-here-interpolation", "sample/main.ps1", "$value = @\"\n$(Get-Value # prose)\n\"@\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := sourceCommentFindingsFor(t, testCase.path, testCase.source)
			if len(findings) != 1 || findings[0].Check != "policy.sourceCommentCoverage" {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestSourceCommentFStringAndHeredocSubstitutions(t *testing.T) {
	t.Parallel()
	python := sourceCommentFindingsFor(t, "sample/main.py", "value = f\"\"\"{value # prose\n}\"\"\"\n")
	if len(python) != 1 || python[0].Check != "policy.sourceComment" || python[0].Subject != "1:20" {
		t.Fatalf("python findings = %+v", python)
	}
	shell := sourceCommentFindingsFor(t, "sample/main.sh", "cat <<EOF\n$(printf value # prose\n)\nEOF\n")
	if len(shell) != 1 || shell[0].Check != "policy.sourceComment" || shell[0].Subject != "2:16" {
		t.Fatalf("shell findings = %+v", shell)
	}
	quoted := sourceCommentFindingsFor(t, "sample/quoted.sh", "cat <<'EOF'\n$(printf value # prose\n)\nEOF\n")
	if len(quoted) != 0 {
		t.Fatalf("quoted findings = %+v", quoted)
	}
}

func TestPowerShellInterpolatedSubexpressionsExposeComments(t *testing.T) {
	t.Parallel()
	stringFindings := sourceCommentFindingsFor(t, "sample/string.ps1", "$value = \"$(Get-Value # prose\n)\"\n")
	if len(stringFindings) != 1 || stringFindings[0].Check != "policy.sourceComment" || stringFindings[0].Subject != "1:23" {
		t.Fatalf("string findings = %+v", stringFindings)
	}
	hereFindings := sourceCommentFindingsFor(t, "sample/here.ps1", "$value = @\"\n$(Get-Value # prose\n)\n\"@\n")
	if len(hereFindings) != 1 || hereFindings[0].Check != "policy.sourceComment" || hereFindings[0].Subject != "2:13" {
		t.Fatalf("here findings = %+v", hereFindings)
	}
}

func TestSourceCommentCoverageIsFirstClassAndGeneratedIsExempt(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Scope.Languages = []policy.LanguageRule{{Name: "elixir", Paths: []string{"lib/**/*.ex"}}}
	writeQualityFile(t, repo.Root, "lib/sample.ex", "# prose\n")
	coverage := CoverageFindings(repo, []string{"lib/sample.ex"})
	if !slices.ContainsFunc(coverage, func(finding policy.Finding) bool {
		return finding.Check == "policy.sourceCommentCoverage" && finding.Path == "lib/sample.ex" && finding.Subject == "elixir"
	}) {
		t.Fatalf("coverage = %+v", coverage)
	}
	if findings := sourceCommentFindings(repo, []string{"lib/sample.ex"}); len(findings) != 0 {
		t.Fatalf("source findings = %+v", findings)
	}
	writeQualityFile(t, repo.Root, "generated/sample.css", "/* prose */\n")
	repo.Config.Scope.Generated = []string{"generated/**"}
	if findings := sourceCommentFindings(repo, []string{"generated/sample.css"}); len(findings) != 0 {
		t.Fatalf("generated findings = %+v", findings)
	}
	if findings := sourceCommentCoverageFindings(repo, []string{"generated/sample.css"}); len(findings) != 0 {
		t.Fatalf("generated coverage = %+v", findings)
	}
	writeQualityFile(t, repo.Root, "sample/main.ts", "// owned by the JavaScript parser\n")
	if findings := sourceCommentFindings(repo, []string{"sample/main.ts"}); len(findings) != 0 {
		t.Fatalf("typescript findings = %+v", findings)
	}
	if findings := sourceCommentCoverageFindings(repo, []string{"sample/main.ts"}); len(findings) != 0 {
		t.Fatalf("typescript coverage = %+v", findings)
	}
	allowComments := true
	repo.Config.Quality.AllowComments = &allowComments
	if findings := sourceCommentCoverageFindings(repo, []string{"lib/sample.ex"}); len(findings) != 0 {
		t.Fatalf("allowComments coverage = %+v", findings)
	}
}

func sourceCommentFindingsFor(t *testing.T, path, source string) []policy.Finding {
	t.Helper()
	repo := qualityRepository(t)
	writeQualityFile(t, repo.Root, path, source)
	return sourceCommentFindings(repo, []string{path})
}

func sourceCommentSubjects(findings []policy.Finding) []string {
	subjects := make([]string, 0, len(findings))
	for _, finding := range findings {
		subjects = append(subjects, finding.Subject)
	}
	return subjects
}
