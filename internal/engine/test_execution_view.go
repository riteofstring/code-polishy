package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

type suiteExecutionView struct {
	root        string
	files       map[string]testreceipt.Input
	directories map[string]bool
}

func (controller *testReceiptController) PrepareSuiteView(suite policy.TestSuite) (string, func() error, error) {
	identity, found := controller.identities[suite.Name]
	if !suite.Reusable {
		return "", nil, errors.New("suite is not reusable")
	}
	if !found {
		return controller.root, func() error { return nil }, nil
	}
	if err := validateReusableArguments(suite.Argv); err != nil {
		return "", nil, err
	}
	view, err := createSuiteExecutionView(controller.root, suite.Cwd, identity.Inputs)
	if err != nil {
		return "", nil, err
	}
	return view.root, view.close, nil
}

func validateReusableArguments(argv []string) error {
	for _, argument := range argv[1:] {
		if strings.Contains(argument, "{artifactDir}") {
			continue
		}
		value := argument
		if _, remainder, found := strings.Cut(argument, "="); found {
			value = remainder
		}
		if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
			return fmt.Errorf("reusable suite argument %q names an ambient absolute path", argument)
		}
	}
	return nil
}

func createSuiteExecutionView(repositoryRoot, cwd string, inputs []testreceipt.Input) (*suiteExecutionView, error) {
	root, err := os.MkdirTemp("", "code-polishy-test-view-")
	if err != nil {
		return nil, err
	}
	view := &suiteExecutionView{root: root, files: map[string]testreceipt.Input{}, directories: map[string]bool{".": true}}
	valid := false
	defer func() {
		if !valid {
			_ = view.remove()
		}
	}()
	for _, input := range inputs {
		if err := view.copyInput(repositoryRoot, input); err != nil {
			return nil, err
		}
	}
	if cwd == "" {
		cwd = "."
	}
	view.addDirectory(filepath.Clean(filepath.FromSlash(cwd)))
	if err := view.seal(); err != nil {
		return nil, err
	}
	valid = true
	return view, nil
}

func (view *suiteExecutionView) copyInput(repositoryRoot string, input testreceipt.Input) error {
	relative, err := view.validatedInputPath(input)
	if err != nil {
		return err
	}
	view.addDirectory(filepath.Dir(relative))
	source := filepath.Join(repositoryRoot, relative)
	if !suiteViewInputMatches(source, input) {
		return fmt.Errorf("reusable suite input %q changed before execution", input.Path)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(view.root, relative)), 0o700); err != nil {
		return err
	}
	destination := filepath.Join(view.root, relative)
	if err := copySuiteViewFile(source, destination); err != nil {
		return err
	}
	digest, err := suiteViewFileSHA256(destination)
	if err != nil || digest != input.SHA256 {
		return fmt.Errorf("reusable suite input %q changed before execution", input.Path)
	}
	view.files[filepath.ToSlash(relative)] = input
	return nil
}

func (view *suiteExecutionView) validatedInputPath(input testreceipt.Input) (string, error) {
	relative := filepath.Clean(filepath.FromSlash(input.Path))
	outside := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
	_, duplicate := view.files[filepath.ToSlash(relative)]
	if relative == "." || filepath.IsAbs(relative) || outside || duplicate {
		return "", fmt.Errorf("reusable suite input %q is invalid or duplicated", input.Path)
	}
	return relative, nil
}

func suiteViewInputMatches(path string, input testreceipt.Input) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && uint32(info.Mode()) == input.Mode
}

func copySuiteViewFile(source, destination string) error {
	inputFile, err := os.Open(source)
	if err != nil {
		return err
	}
	outputFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Join(err, inputFile.Close())
	}
	_, copyErr := io.Copy(outputFile, inputFile)
	return errors.Join(copyErr, inputFile.Close(), outputFile.Close())
}

func (view *suiteExecutionView) addDirectory(relative string) {
	for relative != "." && relative != "" {
		view.directories[filepath.ToSlash(relative)] = true
		relative = filepath.Dir(relative)
	}
	view.directories["."] = true
}

func (view *suiteExecutionView) seal() error {
	for path, input := range view.files {
		mode := os.FileMode(input.Mode).Perm() &^ 0o222
		if err := os.Chmod(filepath.Join(view.root, filepath.FromSlash(path)), mode); err != nil {
			return err
		}
	}
	directories := make([]string, 0, len(view.directories))
	for directory := range view.directories {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(left, right int) bool { return len(directories[left]) > len(directories[right]) })
	for _, directory := range directories {
		if err := os.MkdirAll(filepath.Join(view.root, filepath.FromSlash(directory)), 0o700); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(view.root, filepath.FromSlash(directory)), 0o555); err != nil {
			return err
		}
	}
	return nil
}

func (view *suiteExecutionView) close() error {
	verification := view.verify()
	return errors.Join(verification, view.remove())
}

func (view *suiteExecutionView) verify() error {
	seen := map[string]bool{}
	err := filepath.WalkDir(view.root, func(path string, entry os.DirEntry, walkErr error) error {
		return view.verifyEntry(path, entry, walkErr, seen)
	})
	if err != nil {
		return err
	}
	if len(seen) != len(view.files) {
		return errors.New("reusable suite removed a declared input")
	}
	return nil
}

func (view *suiteExecutionView) verifyEntry(path string, entry os.DirEntry, walkErr error, seen map[string]bool) error {
	if walkErr != nil {
		return walkErr
	}
	relative, err := filepath.Rel(view.root, path)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(relative)
	if entry.IsDir() {
		return view.verifyDirectory(relative)
	}
	input, found := view.files[relative]
	if !found || !regularSuiteViewEntry(entry) {
		return fmt.Errorf("reusable suite wrote undeclared or special path %q", relative)
	}
	digest, digestErr := suiteViewFileSHA256(path)
	if digestErr != nil || digest != input.SHA256 {
		return fmt.Errorf("reusable suite changed declared input %q", relative)
	}
	seen[relative] = true
	return nil
}

func (view *suiteExecutionView) verifyDirectory(relative string) error {
	if !view.directories[relative] {
		return fmt.Errorf("reusable suite wrote undeclared directory %q", relative)
	}
	return nil
}

func regularSuiteViewEntry(entry os.DirEntry) bool {
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func (view *suiteExecutionView) remove() error {
	_ = filepath.WalkDir(view.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(view.root)
}

func suiteViewFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
