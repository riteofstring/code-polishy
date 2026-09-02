package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const maximumPackBytes = 128 << 20
const maximumPackFileBytes = 16 << 20
const maximumPackFiles = 10000

type Identity struct {
	Name    string
	Version string
	Digest  string
}

type Receipt struct {
	ReceiptVersion int            `json:"receiptVersion"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Digest         string         `json:"digest"`
	Files          []ReceiptEntry `json:"files"`
}

type ReceiptEntry struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}

type sourceTree struct {
	Manifest Manifest
	Files    []sourceFile
	Receipt  Receipt
}

type sourceFile struct {
	Path       string
	Data       []byte
	Executable bool
}

func Install(source, dataRoot string) (Identity, string, error) {
	tree, err := readSourceTree(source, true)
	if err != nil {
		return Identity{}, "", err
	}
	target := InstalledRoot(dataRoot, tree.Receipt.Name, tree.Receipt.Version, tree.Receipt.Digest)
	exists, err := existingInstall(target)
	if err != nil {
		return Identity{}, "", err
	}
	if exists {
		return identityFor(tree.Receipt), target, nil
	}
	if err := publishTree(tree, target); err != nil {
		return Identity{}, "", err
	}
	return identityFor(tree.Receipt), target, nil
}

func existingInstall(target string) (bool, error) {
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := VerifyInstalled(target); err != nil {
		return false, fmt.Errorf("installed pack target is corrupt: %w", err)
	}
	return true, nil
}

func publishTree(tree sourceTree, target string) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".pack-candidate-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			makeWritable(staging)
			_ = os.RemoveAll(staging)
		}
	}()
	if err := writeCandidate(staging, tree); err != nil {
		return err
	}
	if _, err := VerifyInstalled(staging); err != nil {
		return err
	}
	if err := freezeTreeContents(staging, tree.Receipt); err != nil {
		return err
	}
	if err := os.Rename(staging, target); err != nil {
		if _, verifyErr := VerifyInstalled(target); verifyErr != nil {
			return err
		}
		return nil
	}
	if err := os.Chmod(target, 0o555); err != nil {
		makeWritable(target)
		_ = os.RemoveAll(target)
		return err
	}
	keep = true
	return nil
}

func writeCandidate(staging string, tree sourceTree) error {
	for _, file := range tree.Files {
		installed := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if file.Executable {
			mode = 0o755
		}
		if err := os.WriteFile(installed, file.Data, mode); err != nil {
			return err
		}
	}
	receipt, err := json.MarshalIndent(tree.Receipt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(staging, ReceiptFilename), append(receipt, '\n'), 0o644)
}

func freezeTreeContents(root string, receipt Receipt) error {
	for _, entry := range receipt.Files {
		mode := fs.FileMode(0o444)
		if entry.Executable {
			mode = 0o555
		}
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(entry.Path)), mode); err != nil {
			return err
		}
	}
	if err := os.Chmod(filepath.Join(root, ReceiptFilename), 0o444); err != nil {
		return err
	}
	directories := []string{}
	if err := filepath.WalkDir(root, func(candidate string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			directories = append(directories, candidate)
		}
		return err
	}); err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if directories[index] == root {
			continue
		}
		if err := os.Chmod(directories[index], 0o555); err != nil {
			return err
		}
	}
	return nil
}

func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(candidate string, entry fs.DirEntry, err error) error {
		if err == nil {
			if entry.IsDir() {
				_ = os.Chmod(candidate, 0o755)
			} else {
				_ = os.Chmod(candidate, 0o644)
			}
		}
		return nil
	})
}

func ReadSource(source string) (Manifest, error) {
	tree, err := readSourceTree(source, false)
	return tree.Manifest, err
}

func readSourceTree(source string, enforcePlatform bool) (sourceTree, error) {
	root, err := canonicalDirectory(source)
	if err != nil {
		return sourceTree{}, err
	}
	files, err := inventorySource(root)
	if err != nil {
		return sourceTree{}, err
	}
	manifest, executables, err := validateSourceFiles(root, files, enforcePlatform)
	if err != nil {
		return sourceTree{}, err
	}
	receipt := buildReceipt(manifest, files, executables)
	return sourceTree{Manifest: manifest, Files: files, Receipt: receipt}, nil
}

type sourceInventory struct {
	root  string
	files []sourceFile
	total int
}

func inventorySource(root string) ([]sourceFile, error) {
	inventory := &sourceInventory{root: root}
	if err := filepath.WalkDir(root, inventory.visit); err != nil {
		return nil, err
	}
	sort.Slice(inventory.files, func(left, right int) bool { return inventory.files[left].Path < inventory.files[right].Path })
	return inventory.files, rejectCaseCollisions(inventory.files)
}

func (inventory *sourceInventory) visit(candidate string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pack source contains symlink %s", candidate)
	}
	if entry.IsDir() {
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("pack source contains special file %s", candidate)
	}
	if len(inventory.files) >= maximumPackFiles || info.Size() > maximumPackFileBytes || inventory.total+int(info.Size()) > maximumPackBytes {
		return errors.New("pack source exceeds its bounded file or tree size")
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(inventory.root, candidate)
	if err != nil {
		return err
	}
	inventory.files = append(inventory.files, sourceFile{Path: filepath.ToSlash(relative), Data: data})
	inventory.total += len(data)
	return nil
}

func rejectCaseCollisions(files []sourceFile) error {
	caseFolded := map[string]bool{}
	for _, file := range files {
		folded := strings.ToLower(file.Path)
		if caseFolded[folded] {
			return fmt.Errorf("pack source contains case-colliding path %s", file.Path)
		}
		caseFolded[folded] = true
	}
	return nil
}

func validateSourceFiles(root string, files []sourceFile, enforcePlatform bool) (Manifest, map[string]bool, error) {
	manifestData, ok := sourceData(files, ManifestFilename)
	if !ok {
		return Manifest{}, nil, errors.New("pack source has no code-polishy-pack.json")
	}
	if _, ok := sourceData(files, "README.md"); !ok {
		return Manifest{}, nil, errors.New("pack source has no README.md")
	}
	manifest, err := ParseManifest(manifestData, filepath.Join(root, ManifestFilename))
	if err != nil {
		return Manifest{}, nil, err
	}
	if enforcePlatform && !slices.Contains(manifest.Platforms, CurrentPlatform()) {
		return Manifest{}, nil, fmt.Errorf("pack %s %s does not support %s", manifest.Name, manifest.Version, CurrentPlatform())
	}
	executables, err := validateSourceCommands(files, manifest.Commands)
	if err != nil {
		return Manifest{}, nil, err
	}
	if err := validateSourceFixtures(files, manifest.Fixtures); err != nil {
		return Manifest{}, nil, err
	}
	return manifest, executables, nil
}

func validateSourceCommands(files []sourceFile, commands []Command) (map[string]bool, error) {
	executables := map[string]bool{}
	for _, command := range commands {
		executables[command.Argv[0]] = true
		if sourceFileByPath(files, command.Argv[0]) == nil {
			return nil, fmt.Errorf("pack command %q is missing %s", command.Name, command.Argv[0])
		}
	}
	return executables, nil
}

func validateSourceFixtures(files []sourceFile, fixtures []Fixture) error {
	for _, fixture := range fixtures {
		project := fixture.Project + "/"
		if !slices.ContainsFunc(files, func(file sourceFile) bool { return strings.HasPrefix(file.Path, project) }) {
			return fmt.Errorf("fixture %q project is missing or empty", fixture.Name)
		}
		for _, selected := range fixture.Files {
			if sourceFileByPath(files, pathJoin(fixture.Project, selected)) == nil {
				return fmt.Errorf("fixture %q selected file %q is missing", fixture.Name, selected)
			}
		}
	}
	return nil
}

func buildReceipt(manifest Manifest, files []sourceFile, executables map[string]bool) Receipt {
	entries := make([]ReceiptEntry, 0, len(files))
	for index := range files {
		files[index].Executable = executables[files[index].Path]
		digest := sha256.Sum256(files[index].Data)
		entries = append(entries, ReceiptEntry{Path: files[index].Path, SHA256: hex.EncodeToString(digest[:]), Executable: files[index].Executable})
	}
	receipt := Receipt{ReceiptVersion: 1, Name: manifest.Name, Version: manifest.Version, Files: entries}
	receipt.Digest = receiptDigest(receipt)
	return receipt
}

func VerifyInstalled(root string) (Receipt, error) {
	canonical, err := canonicalDirectory(root)
	if err != nil {
		return Receipt{}, err
	}
	receipt, err := readReceipt(canonical)
	if err != nil {
		return Receipt{}, err
	}
	pending, err := receiptEntries(receipt)
	if err != nil {
		return Receipt{}, err
	}
	if err := verifyInstalledFiles(canonical, pending); err != nil {
		return Receipt{}, err
	}
	if len(pending) != 0 {
		return Receipt{}, errors.New("installed pack is missing recorded files")
	}
	return receipt, nil
}

func readReceipt(canonical string) (Receipt, error) {
	receiptPath := filepath.Join(canonical, ReceiptFilename)
	receiptInfo, err := os.Lstat(receiptPath)
	if err != nil || !receiptInfo.Mode().IsRegular() || receiptInfo.Mode()&os.ModeSymlink != 0 {
		return Receipt{}, errors.New("pack receipt is missing or is not a regular file")
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		return Receipt{}, fmt.Errorf("read pack receipt: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	receipt := Receipt{}
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf("parse pack receipt: %w", err)
	}
	if receipt.ReceiptVersion != 1 || !identifierPattern.MatchString(receipt.Name) || !semanticVersionPattern.MatchString(receipt.Version) || receipt.Digest != receiptDigest(receipt) {
		return Receipt{}, errors.New("pack receipt identity or digest is invalid")
	}
	return receipt, nil
}

func receiptEntries(receipt Receipt) (map[string]ReceiptEntry, error) {
	pending := map[string]ReceiptEntry{}
	for _, entry := range receipt.Files {
		if err := exactRelativePath(entry.Path); err != nil || pending[entry.Path].Path != "" {
			return nil, errors.New("pack receipt contains an invalid or duplicate path")
		}
		pending[entry.Path] = entry
	}
	return pending, nil
}

func verifyInstalledFiles(canonical string, pending map[string]ReceiptEntry) error {
	return filepath.WalkDir(canonical, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(canonical, candidate)
		relative = filepath.ToSlash(relative)
		if relative == ReceiptFilename {
			return nil
		}
		recorded, ok := pending[relative]
		if !ok || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("installed pack contains unrecorded or unsafe file %s", relative)
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != recorded.SHA256 {
			return fmt.Errorf("installed pack file %s does not match its receipt", relative)
		}
		delete(pending, relative)
		return nil
	})
}

func canonicalDirectory(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("pack root is not a regular directory: %s", absolute)
	}
	return filepath.EvalSymlinks(absolute)
}

func receiptDigest(receipt Receipt) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "receiptVersion=%d\nname=%s\nversion=%s\n", receipt.ReceiptVersion, receipt.Name, receipt.Version)
	for _, entry := range receipt.Files {
		fmt.Fprintf(hash, "%s\x00%s\x00%t\n", entry.Path, entry.SHA256, entry.Executable)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sourceData(files []sourceFile, name string) ([]byte, bool) {
	file := sourceFileByPath(files, name)
	if file == nil {
		return nil, false
	}
	return file.Data, true
}

func sourceFileByPath(files []sourceFile, name string) *sourceFile {
	index, found := slices.BinarySearchFunc(files, name, func(file sourceFile, target string) int { return strings.Compare(file.Path, target) })
	if !found {
		return nil
	}
	return &files[index]
}

func identityFor(receipt Receipt) Identity {
	return Identity{Name: receipt.Name, Version: receipt.Version, Digest: receipt.Digest}
}

func pathJoin(base, relative string) string {
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(relative, "/")
}
