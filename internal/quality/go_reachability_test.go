package quality

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestGoReachabilityReportsFunctionsUnreachableOnEveryPlatform(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/reachability\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "cmd/app/main.go", "package main\n\nimport \"example.test/reachability/lib\"\n\nfunc main() {\n\tlib.Used()\n}\n")
	writeQualityFile(t, repo.Root, "cmd/app/main_windows.go", "package main\n\nimport \"example.test/reachability/lib\"\n\nfunc init() {\n\tlib.WindowsOnly()\n}\n")
	writeQualityFile(t, repo.Root, "cmd/app/main_amd64.go", "package main\n\nimport \"example.test/reachability/lib\"\n\nfunc init() {\n\tlib.AMD64Only()\n}\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc Used() {}\n\nfunc WindowsOnly() {}\n\nfunc Abandoned() {}\n\nfunc AMD64Only() {}\n")
	writeQualityFile(t, repo.Root, "lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestAbandoned(t *testing.T) {\n\tAbandoned()\n}\n")
	commandRunner := runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}
	selection := repository.Selection{Files: []string{"lib/lib.go"}}
	if findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "check"); len(findings) != 0 {
		t.Fatalf("a focused check reached a whole-program conclusion: %+v", findings)
	}
	findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Check != "quality.deadCode" || findings[0].Path != "lib/lib.go" || findings[0].Subject != "Abandoned" || findings[0].Line != 7 {
		t.Fatalf("findings = %+v", findings)
	}
	planned := goReachabilityCommands(repo, repository.Selection{All: true, Files: selection.Files}, "check")
	if len(planned) != 5 || !crossTargetsDisableCgo(planned) {
		t.Fatalf("complete selection plan = %+v", planned)
	}
}

func TestGoReachabilitySkipsModulesWithoutPrograms(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/library\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc Exported() {}\n")
	if commands := goReachabilityCommands(repo, repository.Selection{All: true, Files: []string{"lib/lib.go"}}, "gate"); len(commands) != 0 {
		t.Fatalf("a library module has no program roots: %+v", commands)
	}
}

func TestGoReachabilityHonorsBuildConstraintsOnProgramRoots(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		program string
		want    []string
	}{
		"ignored generator":  {"//go:build ignore\n\npackage main\n\nimport \"example.test/constrained/lib\"\n\nfunc main() {\n\tlib.Used()\n}\n", nil},
		"linux-only program": {"//go:build linux\n\npackage main\n\nimport \"example.test/constrained/lib\"\n\nfunc main() {\n\tlib.Used()\n}\n", []string{"Abandoned"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := cargoQualityRepository(t)
			writeQualityFile(t, repo.Root, "go.mod", "module example.test/constrained\n\ngo 1.26\n")
			writeQualityFile(t, repo.Root, "cmd/tool/main.go", test.program)
			writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc Used() {}\n\nfunc Abandoned() {}\n")
			commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
			findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"lib/lib.go"}}, commandRunner, "check")
			subjects := []string{}
			for _, finding := range findings {
				if finding.Check != "quality.deadCode" || finding.Path != "lib/lib.go" {
					t.Fatalf("findings = %+v", findings)
				}
				subjects = append(subjects, finding.Subject)
			}
			if !slices.Equal(subjects, test.want) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestGoReachabilityIgnoresSeparateModulesWithTheSamePathPrefix(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/review\n\ngo 1.26\n\nrequire example.test/review/sdk v0.0.0\n\nreplace example.test/review/sdk => ./sdk\n")
	writeQualityFile(t, repo.Root, "sdk/go.mod", "module example.test/review/sdk\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "sdk/sdk.go", "package sdk\n\nfunc Used() {}\n\nfunc Unused() {}\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport \"example.test/review/sdk\"\n\nfunc main() {\n\tsdk.Used()\n}\n\nfunc abandoned() {}\n")
	commandRunner := runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "main.go" || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityCountsCallersInModulesThatCompileTheSource(t *testing.T) {
	t.Parallel()
	caller := "package main\n\nimport \"example.test/library\"\n\nfunc main() {\n\tlibrary.UsedByApp()\n}\n"
	for name, layout := range map[string]map[string]string{
		"workspace": {
			"go.work":     "go 1.26\n\nuse (\n\t./library\n\t./app\n)\n",
			"app/go.mod":  "module example.test/app\n\ngo 1.26\n\nrequire example.test/library v0.0.0\n",
			"app/main.go": caller,
		},
		"replacement": {
			"app/go.mod":  "module example.test/app\n\ngo 1.26\n\nrequire example.test/library v0.0.0\n\nreplace example.test/library => ../library\n",
			"app/main.go": caller,
		},
		"replacement beside a workspace": {
			"go.work":     "go 1.26\n\nuse ./library\n",
			"app/go.mod":  "module example.test/app\n\ngo 1.26\n\nrequire example.test/library v0.0.0\n\nreplace example.test/library => ../library\n",
			"app/main.go": caller,
		},
		"aliased replacement": {
			"app/go.mod":  "module example.test/app\n\ngo 1.26\n\nrequire example.test/alias v0.0.0\n\nreplace example.test/alias => ../library\n",
			"app/main.go": "package main\n\nimport \"example.test/alias\"\n\nfunc main() {\n\tlibrary.UsedByApp()\n}\n",
		},
		"unloadable caller": {
			"go.work":     "go 1.26\n\nuse (\n\t./library\n\t./app\n)\n",
			"app/go.mod":  "module example.test/app\n\ngo 1.26\n\nrequire example.test/library v0.0.0\n",
			"app/main.go": "package main\n\nimport (\n\t\"example.test/library\"\n\t\"example.test/missing\"\n)\n\nfunc main() {\n\tlibrary.UsedByApp()\n\tmissing.Run()\n}\n",
		},
	} {
		for scope, exclude := range map[string][]string{"included": nil, "excluded": {"app/**"}} {
			t.Run(name+"/"+scope, func(t *testing.T) {
				t.Parallel()
				repo := cargoQualityRepository(t)
				repo.Config.Scope.Exclude = exclude
				writeQualityFile(t, repo.Root, "library/go.mod", "module example.test/library\n\ngo 1.26\n")
				writeQualityFile(t, repo.Root, "library/library.go", "package library\n\nfunc UsedByApp() {}\n\nfunc Abandoned() {}\n")
				writeQualityFile(t, repo.Root, "library/cmd/tool/main.go", "package main\n\nfunc main() {}\n")
				for path, content := range layout {
					writeQualityFile(t, repo.Root, path, content)
				}
				commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
				findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"library/library.go"}}, commandRunner, "gate")
				subjects := []string{}
				for _, finding := range findings {
					subjects = append(subjects, finding.Check+":"+finding.Path+":"+finding.Subject)
				}
				want := []string{"quality.deadCode:library/library.go:Abandoned"}
				if name == "unloadable caller" {
					want = []string{}
					for _, target := range goReachabilityTargets {
						want = append(want, "quality.deadCodeCoverage:app/go.mod:"+target)
					}
				}
				if !slices.Equal(subjects, want) {
					t.Fatalf("findings = %+v", findings)
				}
			})
		}
	}
}

func TestGoReachabilityIgnoresWorkspaceNonmembers(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	repo.Config.Scope.Exclude = []string{"fixtures/**"}
	writeQualityFile(t, repo.Root, "go.work", "go 1.26\n\nuse (\n\t./library\n\t./app\n)\n")
	writeQualityFile(t, repo.Root, "library/go.mod", "module example.test/library\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "library/library.go", "package library\n\nfunc UsedByApp() {}\n\nfunc Abandoned() {}\n")
	writeQualityFile(t, repo.Root, "library/cmd/tool/main.go", "package main\n\nfunc main() {}\n")
	writeQualityFile(t, repo.Root, "app/go.mod", "module example.test/app\n\ngo 1.26\n\nrequire example.test/library v0.0.0\n")
	writeQualityFile(t, repo.Root, "app/main.go", "package main\n\nimport \"example.test/library\"\n\nfunc main() {\n\tlibrary.UsedByApp()\n}\n")
	writeQualityFile(t, repo.Root, "fixtures/go.mod", "module example.test/fixtures\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "fixtures/main.go", "package main\n\nfunc main() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"library/library.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Check != "quality.deadCode" || findings[0].Path != "library/library.go" || findings[0].Subject != "Abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoListedPackageFilesKeepsOnlyRepositorySources(t *testing.T) {
	t.Parallel()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "repo")
	for name, directory := range map[string]string{
		"root":           root,
		"nested":         filepath.Join(root, "library"),
		"external":       filepath.Join(parent, "dependency"),
		"similar prefix": root + "-dependency",
	} {
		t.Run(name, func(t *testing.T) {
			writeQualityFile(t, directory, "source.go", "package library\n")
			writeQualityFile(t, directory, "native.go", "package library\n\nimport \"C\"\n")
			_, paths, err := goListedPackageFiles(root, goListPackage{
				Dir: directory, GoFiles: []string{"source.go"}, CgoFiles: []string{"native.go"},
			})
			want := []string{}
			switch name {
			case "root":
				want = []string{"source.go", "native.go"}
			case "nested":
				want = []string{"library/source.go", "library/native.go"}
			}
			if err != nil || !slices.Equal(paths, want) {
				t.Fatalf("package files = %v, error = %v; want %v", paths, err, want)
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Run("different drive", func(t *testing.T) {
			drive := "Z:"
			if strings.EqualFold(filepath.VolumeName(root), drive) {
				drive = "Y:"
			}
			_, paths, err := goListedPackageFiles(drive+`\checkout`, goListPackage{
				Dir: root, GoFiles: []string{"source.go"}, CgoFiles: []string{"native.go"},
			})
			if err != nil || len(paths) != 0 {
				t.Fatalf("external drive package files = %v, error = %v", paths, err)
			}
		})
	}
}

func TestGoReachabilityPlansOnlyTargetsThatBuildAProgram(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/linuxonly\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main_linux.go", "package main\n\nfunc main() {\n\thelper()\n}\n\nfunc abandoned() {}\n")
	writeQualityFile(t, repo.Root, "helper.go", "package main\n\nfunc helper() {}\n")
	writeQualityFile(t, repo.Root, "testdata/fixture/main.go", "package main\n\nfunc main() {}\n")
	selection := repository.Selection{All: true, Files: []string{"main_linux.go"}}
	planned := []string{}
	for _, command := range goReachabilityCommands(repo, selection, "gate") {
		planned = append(planned, strings.Join(command.EnvironmentOverrides[:2], " "))
	}
	if !slices.Equal(planned, []string{"GOOS=linux GOARCH=amd64", "GOOS=linux GOARCH=arm64"}) {
		t.Fatalf("planned targets = %v", planned)
	}
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "main_linux.go" || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

type failingCommandRunner struct {
	t        *testing.T
	delegate runner.Runner
}

func (recording *failingCommandRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	result, output, err := recording.delegate.Execute(ctx, invocation)
	if err != nil {
		recording.t.Fatalf("a gate would record %q as failed: %v: %s", invocation.Command.Name, err, output.Stderr)
	}
	return result, output, err
}

func TestGoReachabilityReportsDeadFunctionsCompiledOnlyForOneTarget(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/specific\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport \"example.test/specific/lib\"\n\nfunc main() {\n\tlib.Used()\n}\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc Used() {}\n")
	writeQualityFile(t, repo.Root, "lib/helper_windows.go", "package lib\n\nfunc WindowsUnused() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "lib/helper_windows.go" || findings[0].Subject != "WindowsUnused" ||
		!strings.HasSuffix(findings[0].Message, "on windows/amd64") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityAnalyzesCgoProgramsOnTheHostTarget(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	requireHostCgo(t, repo)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/native\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\n// static int answer(void) { return 42; }\nimport \"C\"\n\nfunc main() {\n\t_ = C.answer()\n}\n\nfunc abandoned() {}\n")
	selection := repository.Selection{All: true, Files: []string{"main.go"}}
	planned := goReachabilityCommands(repo, selection, "gate")
	if len(planned) != 1 || !slices.Equal(planned[0].EnvironmentOverrides, []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH, "CGO_ENABLED=1", "GOWORK=off"}) {
		t.Fatalf("planned = %+v", planned)
	}
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityKeepsFunctionsCalledFromCInEveryProgram(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	requireHostCgo(t, repo)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/callback\n\ngo 1.26\n")
	for _, program := range []string{"a", "b"} {
		directory := "cmd/" + program + "/tool/"
		writeQualityFile(t, repo.Root, directory+"main.go", "package main\n\n/*\nextern void CalledFromC(void);\nstatic void invoke(void) { CalledFromC(); }\n*/\nimport \"C\"\n\nfunc main() {\n\tC.invoke()\n}\n")
		writeQualityFile(t, repo.Root, directory+"export.go", "package main\n\nimport \"C\"\n\nimport \"example.test/callback/lib\"\n\n//export CalledFromC\nfunc CalledFromC() {\n\tlib.Assist"+strings.ToUpper(program)+"()\n}\n")
	}
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc AssistA() {}\n\nfunc AssistB() {}\n\nfunc Abandoned() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"cmd/a/tool/main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "lib/lib.go" || findings[0].Subject != "Abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityKeepsFunctionsCalledThroughLinkname(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/linked\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport (\n\t_ \"unsafe\"\n\n\t_ \"example.test/linked/lib\"\n)\n\n//go:linkname greet example.test/linked/lib.greet\nfunc greet()\n\nfunc main() {\n\tgreet()\n}\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc greet() {\n\tassist()\n}\n\nfunc assist() {}\n\nfunc Abandoned() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "lib/lib.go" || findings[0].Subject != "Abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilitySkipsTargetsWhoseDependenciesNeedCgo(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	requireHostCgo(t, repo)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/cgodep\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "native/native.go", "package native\n\n// static int answer(void) { return 42; }\nimport \"C\"\n\nfunc Value() int {\n\treturn int(C.answer())\n}\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport \"example.test/cgodep/native\"\n\nfunc main() {\n\t_ = native.Value()\n}\n\nfunc abandoned() {}\n")
	selection := repository.Selection{All: true, Files: []string{"main.go"}}
	planned := goReachabilityCommands(repo, selection, "gate")
	if len(planned) != 1 || !slices.Contains(planned[0].EnvironmentOverrides, "GOOS="+runtime.GOOS) {
		t.Fatalf("planned = %+v", planned)
	}
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityJudgesCgoProgramsOnlyByTargetsThatBuildThem(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	requireHostCgo(t, repo)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/cgoroots\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "cmd/plain/main.go", "package main\n\nfunc main() {}\n")
	writeQualityFile(t, repo.Root, "cmd/native/main.go", "package main\n\nimport \"example.test/cgoroots/native\"\n\nfunc main() {\n\tnative.Run()\n}\n\nfunc abandoned() {}\n")
	writeQualityFile(t, repo.Root, "native/native.go", "package native\n\n// static void noop(void) {}\nimport \"C\"\n\nfunc Run() {\n\tC.noop()\n}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"cmd/plain/main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "cmd/native/main.go" || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityKeepsFunctionsCalledUnderTheCgoConstraint(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	requireHostCgo(t, repo)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/tagged\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nfunc main() {\n\thelper()\n}\n")
	writeQualityFile(t, repo.Root, "helper_cgo.go", "//go:build cgo\n\npackage main\n\nimport \"example.test/tagged/lib\"\n\nfunc helper() {\n\tlib.CgoOnly()\n}\n")
	writeQualityFile(t, repo.Root, "helper_fallback.go", "//go:build !cgo\n\npackage main\n\nfunc helper() {}\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc CgoOnly() {}\n\nfunc Abandoned() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Subject != "Abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityUsesTheInheritedCgoSetting(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/fallback\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nfunc main() {\n\thelper()\n}\n")
	writeQualityFile(t, repo.Root, "native.go", "package main\n\n// static void noop(void) {}\nimport \"C\"\n\nfunc helper() {\n\tC.noop()\n}\n")
	writeQualityFile(t, repo.Root, "fallback.go", "//go:build !cgo\n\npackage main\n\nfunc helper() {}\n\nfunc unused() {}\n")
	selection := repository.Selection{All: true, Files: []string{"main.go"}}
	for _, command := range goReachabilityCommands(repo, selection, "gate") {
		if !slices.Contains(command.EnvironmentOverrides, "CGO_ENABLED=0") {
			t.Fatalf("command ignored the inherited cgo setting: %+v", command)
		}
	}
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, selection, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "fallback.go" || findings[0].Subject != "unused" {
		t.Fatalf("findings = %+v", findings)
	}
}

func crossTargetsDisableCgo(commands []policy.Command) bool {
	host := []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH}
	for _, command := range commands {
		if !slices.Equal(command.EnvironmentOverrides[:2], host) && !slices.Contains(command.EnvironmentOverrides, "CGO_ENABLED=0") {
			return false
		}
	}
	return true
}

func requireHostCgo(t *testing.T, repo repository.Repository) {
	t.Helper()
	if !slices.Contains(goReachabilityTargets, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Skipf("%s/%s is not a supported release target", runtime.GOOS, runtime.GOARCH)
	}
	output, err := runner.ToolOutput(repo.GoTool("go"), "env", "CGO_ENABLED")
	if err != nil || strings.TrimSpace(string(output)) != "1" {
		t.Skipf("host cgo is unavailable: %s %v", output, err)
	}
}

func TestGoReachabilityHonorsScopeExclusions(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	repo.Config.Scope.Exclude = []string{"external/**"}
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/excluded\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "external/vendored/vendored.go", "package vendored\n\nfunc Used() {}\n\nfunc Unused() {}\n")
	writeQualityFile(t, repo.Root, "external/broken/broken.go", "package broken\n\nfunc Broken() { undefined() }\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport \"example.test/excluded/external/vendored\"\n\nfunc main() {\n\tvendored.Used()\n}\n\nfunc abandoned() {}\n")
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	if len(findings) != 1 || findings[0].Path != "main.go" || findings[0].Subject != "abandoned" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoReachabilityLeavesPackOwnedPackagesToTheirProvider(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	repo.Config.Checks = []policy.Command{{
		Name: "custom-dead-code", Provides: []string{"dead-code"}, RunOn: []string{"check", "gate"}, Paths: []string{"plugin/**"},
		Adapter: &policy.PackAdapter{PackName: "custom", Capability: "dead-code"},
	}}
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/owned\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "plugin/plugin.go", "package plugin\n\nfunc Used() {}\n\nfunc Unused() {}\n")
	writeQualityFile(t, repo.Root, "unloaded/unloaded.go", "package unloaded\n\nfunc Unused() {}\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nimport \"example.test/owned/plugin\"\n\nfunc main() {\n\tplugin.Used()\n}\n\nfunc abandoned() {}\n")
	if owner := repo.AnalysisOwner("plugin/plugin.go", "dead-code", "gate"); owner.Native || owner.Pack != "custom" {
		t.Fatalf("plugin owner = %+v", owner)
	}
	commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
	findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"main.go"}}, commandRunner, "gate")
	subjects := []string{}
	for _, finding := range findings {
		subjects = append(subjects, finding.Path+":"+finding.Subject)
	}
	if !slices.Equal(subjects, []string{"main.go:abandoned", "unloaded/unloaded.go:Unused"}) {
		t.Fatalf("findings = %+v", findings)
	}
	for _, command := range goReachabilityCommands(repo, repository.Selection{All: true, Files: []string{"main.go"}}, "gate") {
		if slices.Contains(command.Argv, "./plugin") {
			t.Fatalf("native analysis selected a pack-owned package: %v", command.Argv)
		}
	}
}

func TestGoReachabilityKeepsPackOwnedProgramsAsCallers(t *testing.T) {
	t.Parallel()
	for name, plugin := range map[string]string{
		"loadable":       "package main\n\nimport \"example.test/roots/lib\"\n\nfunc main() {\n\tlib.UsedByPlugin()\n}\n\nfunc pluginOnly() {}\n",
		"unloadable":     "package main\n\nimport (\n\t\"example.test/missing\"\n\t\"example.test/roots/lib\"\n)\n\nfunc main() {\n\tlib.UsedByPlugin()\n\tmissing.Run()\n}\n",
		"unparseable":    "package main\n\nimport \"example.test/roots/lib\"\n\nfunc main() {\n\tlib.UsedByPlugin()\n}\n\nfunc broken( {\n",
		"misconstrained": "//go:build linux &&\n\npackage main\n\nimport \"example.test/roots/lib\"\n\nfunc main() {\n\tlib.UsedByPlugin()\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := cargoQualityRepository(t)
			repo.Config.Checks = []policy.Command{{
				Name: "custom-dead-code", Provides: []string{"dead-code"}, RunOn: []string{"check", "gate"}, Paths: []string{"cmd/plugin/**"},
				Adapter: &policy.PackAdapter{PackName: "custom", Capability: "dead-code"},
			}}
			writeQualityFile(t, repo.Root, "go.mod", "module example.test/roots\n\ngo 1.26\n")
			writeQualityFile(t, repo.Root, "cmd/app/main.go", "package main\n\nimport \"example.test/roots/lib\"\n\nfunc main() {\n\tlib.Used()\n}\n")
			writeQualityFile(t, repo.Root, "cmd/plugin/main.go", plugin)
			writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nfunc Used() {}\n\nfunc UsedByPlugin() {}\n\nfunc Abandoned() {}\n")
			commandRunner := &failingCommandRunner{t: t, delegate: runner.OSRunner{PathEntries: repo.CommandEnvironment().PathEntries}}
			findings := goReachabilityFindings(t.Context(), repo, repository.Selection{All: true, Files: []string{"cmd/app/main.go"}}, commandRunner, "gate")
			subjects := []string{}
			for _, finding := range findings {
				subjects = append(subjects, finding.Check+":"+finding.Subject)
			}
			want := []string{"quality.deadCode:Abandoned"}
			if name != "loadable" {
				want = slices.Repeat([]string{"quality.deadCodeCoverage"}, len(goReachabilityTargets))
				for index, target := range goReachabilityTargets {
					want[index] += ":" + target
				}
			}
			if !slices.Equal(subjects, want) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestGoReachabilityReportsCoverageForNativePackagesThatDoNotLoad(t *testing.T) {
	t.Parallel()
	repo := cargoQualityRepository(t)
	writeQualityFile(t, repo.Root, "go.mod", "module example.test/brokenlib\n\ngo 1.26\n")
	writeQualityFile(t, repo.Root, "main.go", "package main\n\nfunc main() {}\n")
	writeQualityFile(t, repo.Root, "lib/lib.go", "package lib\n\nimport \"example.test/missing\"\n\nfunc Run() {\n\tmissing.Run()\n}\n")
	selection := repository.Selection{All: true, Files: []string{"main.go"}}
	if commands := goReachabilityCommands(repo, selection, "gate"); len(commands) != 0 {
		t.Fatalf("an unloadable program set was analyzed: %+v", commands)
	}
	findings := goReachabilityFindings(t.Context(), repo, selection, runner.OSRunner{}, "gate")
	if len(findings) != len(goReachabilityTargets) || findings[0].Check != "quality.deadCodeCoverage" || !strings.Contains(findings[0].Message, "./lib does not load") {
		t.Fatalf("findings = %+v", findings)
	}
}
