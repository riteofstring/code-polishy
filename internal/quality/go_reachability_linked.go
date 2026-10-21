package quality

import (
	"bytes"
	"context"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const goLinkProbeTimeout = 15 * time.Minute

type goSourceLine struct {
	path string
	line int
}

func goDeclaresLinkerRoots(listed goListPackage, inRepository bool) (bool, error) {
	names, directives := listed.CgoFiles, [][]byte{[]byte("//export ")}
	if inRepository {
		names = append(slices.Clone(listed.GoFiles), listed.CgoFiles...)
		directives = append(directives, []byte("//go:linkname "))
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(listed.Dir, name))
		if err != nil {
			return false, err
		}
		for line := range bytes.Lines(data) {
			if slices.ContainsFunc(directives, func(directive []byte) bool { return bytes.HasPrefix(line, directive) }) {
				return true, nil
			}
		}
	}
	return false, nil
}

func withoutLinkedFunctions(repo repository.Repository, plan goReachabilityPlan, module repository.GoModule, target goBuildTarget, unreachable map[goUnreachableFunction]bool) error {
	if !target.linkerRoots {
		return nil
	}
	linked, err := goLinkedFunctionLines(repo, plan, module, target)
	if err != nil {
		return fmt.Errorf("link the %s programs that export Go functions to C or alias them with go:linkname: %w", target.name, err)
	}
	for function := range unreachable {
		if linked[goSourceLine{path: function.path, line: function.line}] {
			delete(unreachable, function)
		}
	}
	return nil
}

func goLinkedFunctionLines(repo repository.Repository, plan goReachabilityPlan, module repository.GoModule, target goBuildTarget) (map[goSourceLine]bool, error) {
	output, err := os.MkdirTemp("", "code-polishy-linked-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(output)
	ctx, cancel := context.WithTimeout(context.Background(), goLinkProbeTimeout)
	defer cancel()
	environment := target.environment()
	for _, name := range []string{"SDKROOT", "DEVELOPER_DIR"} {
		if value, found := os.LookupEnv(name); found {
			environment = append(environment, name+"="+value)
		}
	}
	linked := map[goSourceLine]bool{}
	for index, program := range target.programs {
		binary := filepath.Join(output, strconv.Itoa(index))
		if _, err := goProbe(ctx, repo, module, environment, "build", "-gcflags=all=-l", "-ldflags=", "-trimpath=false", "-buildvcs=false", "-o", binary, program); err != nil {
			return nil, err
		}
		if err := addLinkedFunctionLines(plan.root, binary, linked); err != nil {
			return nil, fmt.Errorf("read the linked %s: %w", program, err)
		}
	}
	return linked, nil
}

func addLinkedFunctionLines(root, binary string, linked map[goSourceLine]bool) error {
	data, err := goBinaryDWARF(binary)
	if err != nil {
		return err
	}
	reader := data.Reader()
	files := []*dwarf.LineFile{}
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			return err
		}
		switch entry.Tag {
		case dwarf.TagCompileUnit:
			if files, err = goCompileUnitFiles(data, entry); err != nil {
				return err
			}
		case dwarf.TagSubprogram:
			if line, found := goDeclaredLine(root, entry, files); found {
				linked[line] = true
			}
		}
	}
}

func goCompileUnitFiles(data *dwarf.Data, unit *dwarf.Entry) ([]*dwarf.LineFile, error) {
	lines, err := data.LineReader(unit)
	if err != nil || lines == nil {
		return nil, err
	}
	return lines.Files(), nil
}

func goDeclaredLine(root string, entry *dwarf.Entry, files []*dwarf.LineFile) (goSourceLine, bool) {
	file, fileFound := entry.Val(dwarf.AttrDeclFile).(int64)
	line, lineFound := entry.Val(dwarf.AttrDeclLine).(int64)
	if !fileFound || !lineFound || file < 0 || int(file) >= len(files) || files[file] == nil {
		return goSourceLine{}, false
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(files[file].Name))
	if err != nil {
		return goSourceLine{}, false
	}
	relative, err := filepath.Rel(root, filepath.Join(directory, filepath.Base(files[file].Name)))
	if err != nil || !filepath.IsLocal(relative) {
		return goSourceLine{}, false
	}
	return goSourceLine{path: filepath.ToSlash(relative), line: int(line)}, true
}

func goBinaryDWARF(path string) (*dwarf.Data, error) {
	if file, err := elf.Open(path); err == nil {
		defer file.Close()
		return file.DWARF()
	}
	if file, err := macho.Open(path); err == nil {
		defer file.Close()
		return file.DWARF()
	}
	if file, err := pe.Open(path); err == nil {
		defer file.Close()
		return file.DWARF()
	}
	return nil, errors.New("the linked program is not an ELF, Mach-O, or PE executable")
}
