package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const goTargetProbeTimeout = 5 * time.Minute

type goBuildTarget struct {
	name        string
	goos        string
	goarch      string
	cgo         bool
	standalone  bool
	linkerRoots bool
	packages    []string
	programs    []string
	compiled    map[string]bool
}

type goListPackage struct {
	Name       string        `json:"Name"`
	Dir        string        `json:"Dir"`
	GoFiles    []string      `json:"GoFiles"`
	CgoFiles   []string      `json:"CgoFiles"`
	Incomplete bool          `json:"Incomplete"`
	Error      *goListError  `json:"Error"`
	DepsErrors []goListError `json:"DepsErrors"`
}

type goListError struct {
	ImportStack []string `json:"ImportStack"`
	Pos         string   `json:"Pos"`
	Err         string   `json:"Err"`
}

type goParticipant struct {
	pattern string
	entry   bool
}

type goTargetPlan struct {
	target   goBuildTarget
	analyzed bool
	problem  string
}

func (target goBuildTarget) environment() []string {
	cgo := "0"
	if target.cgo {
		cgo = "1"
	}
	environment := []string{"GOOS=" + target.goos, "GOARCH=" + target.goarch, "CGO_ENABLED=" + cgo}
	if target.standalone {
		environment = append(environment, "GOWORK=off")
	}
	return environment
}

func goModuleBuildTargets(repo repository.Repository, module repository.GoModule, build repository.GoBuild) ([]goTargetPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), goTargetProbeTimeout)
	defer cancel()
	root, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return nil, err
	}
	hostCgo, err := goProbe(ctx, repo, module, nil, "env", "CGO_ENABLED")
	if err != nil {
		return nil, fmt.Errorf("read the host cgo setting: %w", err)
	}
	plans := []goTargetPlan{}
	for _, name := range goReachabilityTargets {
		goos, goarch, _ := strings.Cut(name, "/")
		target := goBuildTarget{
			name: name, goos: goos, goarch: goarch, standalone: build.Workspace == "",
			cgo: strings.TrimSpace(string(hostCgo)) == "1" && name == runtime.GOOS+"/"+runtime.GOARCH,
		}
		output, err := goProbe(ctx, repo, module, target.environment(), "list", "-e", "-json=Name,Dir,GoFiles,CgoFiles,Incomplete,Error,DepsErrors", "./...")
		if err != nil {
			return nil, fmt.Errorf("list %s packages: %w", name, err)
		}
		packages, err := decodeGoListPackages(output)
		if err != nil {
			return nil, fmt.Errorf("list %s packages: %w", name, err)
		}
		plan := planGoTarget(repo, root, module, target, packages)
		if plan.analyzed {
			plan.target.compiled, plan.target.linkerRoots, err = goCompiledFiles(ctx, repo, module, root, plan.target)
			if err != nil {
				return nil, fmt.Errorf("list %s dependencies: %w", name, err)
			}
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func goCompiledFiles(ctx context.Context, repo repository.Repository, module repository.GoModule, root string, target goBuildTarget) (map[string]bool, bool, error) {
	output, err := goProbe(ctx, repo, module, target.environment(), append([]string{"list", "-e", "-deps", "-json=Name,Dir,GoFiles,CgoFiles"}, target.packages...)...)
	if err != nil {
		return nil, false, err
	}
	packages, err := decodeGoListPackages(output)
	if err != nil {
		return nil, false, err
	}
	compiled := map[string]bool{}
	linkerRoots := false
	for _, listed := range packages {
		if listed.Dir == "" {
			continue
		}
		_, paths, err := goListedPackageFiles(root, listed)
		if err != nil {
			return nil, false, err
		}
		for _, path := range paths {
			if filepath.IsLocal(filepath.FromSlash(path)) {
				compiled[path] = true
			}
		}
		roots, err := goDeclaresLinkerRoots(listed, paths != nil)
		if err != nil {
			return nil, false, err
		}
		linkerRoots = linkerRoots || roots
	}
	return compiled, linkerRoots, nil
}

func goProbe(ctx context.Context, repo repository.Repository, module repository.GoModule, overrides []string, arguments ...string) ([]byte, error) {
	environment := slices.DeleteFunc(runner.EnvironmentWithPath(nil, repo.CommandEnvironment().PathEntries), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return slices.ContainsFunc(overrides, func(override string) bool { return strings.HasPrefix(override, name+"=") })
	})
	var stdout, stderr bytes.Buffer
	result, err := runner.Run(ctx, runner.HostCommand{
		Path: repo.GoTool("go"), Argv: append([]string{repo.GoTool("go")}, arguments...),
		Directory: filepath.Join(repo.Root, filepath.FromSlash(module.Root)), Environment: append(environment, overrides...),
		Stdout: &stdout, Stderr: &stderr,
	})
	if err == nil && result.ExitStatus != 0 {
		err = fmt.Errorf("go %s exited with status %d", arguments[0], result.ExitStatus)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func decodeGoListPackages(data []byte) ([]goListPackage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	packages := []goListPackage{}
	for {
		var listed goListPackage
		err := decoder.Decode(&listed)
		if errors.Is(err, io.EOF) {
			return packages, nil
		}
		if err != nil {
			return nil, err
		}
		packages = append(packages, listed)
	}
}

func planGoTarget(repo repository.Repository, root string, module repository.GoModule, target goBuildTarget, packages []goListPackage) goTargetPlan {
	moduleDirectory := filepath.Join(root, filepath.FromSlash(module.Root))
	for _, listed := range packages {
		if listed.Incomplete && goExcludedByBuildConfiguration(listed) {
			continue
		}
		participant, err := goTargetParticipant(repo, root, moduleDirectory, target, listed)
		if err != nil {
			return goTargetPlan{target: target, problem: err.Error()}
		}
		if participant.pattern == "" {
			continue
		}
		target.packages = append(target.packages, participant.pattern)
		if participant.entry {
			target.programs = append(target.programs, participant.pattern)
		}
	}
	return goTargetPlan{target: target, analyzed: len(target.programs) > 0}
}

func goTargetParticipant(repo repository.Repository, root string, moduleDirectory string, target goBuildTarget, listed goListPackage) (goParticipant, error) {
	directory, paths, err := goListedPackageFiles(root, listed)
	if err != nil {
		return goParticipant{}, err
	}
	relative, err := filepath.Rel(moduleDirectory, directory)
	if err != nil {
		return goParticipant{}, err
	}
	pattern := goPackagePattern(relative)
	if listed.Error != nil {
		return goParticipant{}, fmt.Errorf("%s does not load for %s: %s", pattern, target.name, listed.Error.Err)
	}
	entry, relevant, err := goPackageRole(repo, listed, paths)
	if err != nil {
		return goParticipant{}, fmt.Errorf("%s does not parse for %s: %w", pattern, target.name, err)
	}
	if !relevant {
		return goParticipant{}, nil
	}
	if listed.Incomplete {
		return goParticipant{}, fmt.Errorf("%s does not load for %s: %s", pattern, target.name, goListErrorMessage(listed))
	}
	return goParticipant{pattern: pattern, entry: entry}, nil
}

func goPackageRole(repo repository.Repository, listed goListPackage, paths []string) (bool, bool, error) {
	entry := false
	if listed.Name == "main" {
		for _, path := range paths {
			declares, err := goDeclaresMain(repo, path)
			if err != nil {
				return false, false, err
			}
			entry = entry || declares
		}
	}
	return entry, entry || slices.ContainsFunc(paths, func(path string) bool { return goNativeDeadCodePath(repo, path) }), nil
}

func goExcludedByBuildConfiguration(listed goListPackage) bool {
	failures := slices.Clone(listed.DepsErrors)
	if listed.Error != nil {
		failures = append(failures, *listed.Error)
	}
	return len(failures) > 0 && !slices.ContainsFunc(failures, func(failure goListError) bool {
		return !strings.HasPrefix(failure.Err, "build constraints exclude all Go files in ")
	})
}

func goListErrorMessage(listed goListPackage) string {
	if len(listed.DepsErrors) > 0 {
		return listed.DepsErrors[0].Err
	}
	return "the package is incomplete"
}

func goNativeDeadCodePath(repo repository.Repository, path string) bool {
	return !repo.IsExcluded(path) && repo.NativeAnalysis(path, "dead-code")
}

func goPackagePattern(relative string) string {
	if relative == "." {
		return "."
	}
	return "./" + filepath.ToSlash(relative)
}

func goListedPackageFiles(root string, listed goListPackage) (string, []string, error) {
	directory, err := filepath.EvalSymlinks(listed.Dir)
	if err != nil {
		return "", nil, err
	}
	if relative, err := filepath.Rel(root, directory); err != nil || !filepath.IsLocal(relative) {
		return directory, nil, nil
	}
	paths := []string{}
	for _, name := range append(slices.Clone(listed.GoFiles), listed.CgoFiles...) {
		relative, err := filepath.Rel(root, filepath.Join(directory, name))
		if err != nil {
			return "", nil, err
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	return directory, paths, nil
}

func goDeclaresMain(repo repository.Repository, path string) (bool, error) {
	data, err := repo.Read(path)
	if err != nil {
		return false, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(file.Decls, func(declaration ast.Decl) bool {
		function, ok := declaration.(*ast.FuncDecl)
		return ok && function.Recv == nil && function.Name.Name == "main" && function.Type.TypeParams == nil
	}), nil
}
