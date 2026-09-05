package release

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func openCapabilityUpgradeRoot(repoRoot string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("open capability upgrade repository: %w", err)
	}
	for _, name := range strings.Split(CapabilityUpgradeDirectory, "/") {
		child, openErr := openCapabilityUpgradeDirectory(root, name, create)
		_ = root.Close()
		if openErr != nil {
			return nil, openErr
		}
		root = child
	}
	return root, nil
}

func openCapabilityUpgradeDirectory(root *os.Root, name string, create bool) (*os.Root, error) {
	if create {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create capability upgrade directory: %w", err)
		}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("capability upgrade directory must not contain symbolic links or non-directories")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = child.Close()
		return nil, fmt.Errorf("capability upgrade directory changed during opening")
	}
	return child, nil
}

func writeCapabilityUpgrade(repoRoot, incomingRoot string, root *os.Root, incoming Lock) (LockUpgradeResult, error) {
	outgoing, present, err := ReadLock(repoRoot)
	if err != nil {
		return LockUpgradeResult{}, err
	}
	if present && sameCapabilityLock(outgoing, incoming) {
		return LockUpgradeResult{Lock: incoming, Delta: ReadCapabilityUpgrade(repoRoot, incoming)}, nil
	}
	var previous *Lock
	if present {
		previous = &outgoing
	}
	record := captureCapabilityUpgrade(incomingRoot, previous, incoming)
	if err := storeCapabilityUpgrade(root, record); err != nil {
		return LockUpgradeResult{}, err
	}
	if err := validateCapabilityUpgradeCutover(repoRoot, incomingRoot, previous, incoming); err != nil {
		return LockUpgradeResult{}, err
	}
	if err := WriteLock(repoRoot, incoming); err != nil {
		return LockUpgradeResult{}, err
	}
	return LockUpgradeResult{Lock: incoming, Changed: true, Delta: record.Delta}, nil
}

func validateCapabilityUpgradeCutover(repoRoot, incomingRoot string, outgoing *Lock, incoming Lock) error {
	current, present, err := ReadLock(repoRoot)
	if err != nil {
		return err
	}
	if !sameOutgoingCapabilityLock(outgoing, current, present) {
		return fmt.Errorf("repository lock changed while preparing the capability upgrade")
	}
	manifest, installed, err := ReadManifest(incomingRoot)
	if err != nil || !installed {
		return fmt.Errorf("incoming release manifest became unavailable during lock preparation")
	}
	return manifest.Satisfies(incoming)
}

func sameOutgoingCapabilityLock(outgoing *Lock, current Lock, present bool) bool {
	if outgoing == nil {
		return !present
	}
	return present && sameCapabilityLock(*outgoing, current)
}

func storeCapabilityUpgrade(root *os.Root, record capabilityUpgradeRecord) error {
	data, err := renderCapabilityUpgradeRecord(record)
	if err != nil {
		return err
	}
	if _, err := parseCapabilityUpgradeRecord(data, record.Delta.Incoming); err != nil {
		return err
	}
	relative := capabilityUpgradeRelativePath(record.Delta.Incoming)
	directory, err := openCapabilityUpgradeDirectory(root, filepath.Dir(relative), true)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := publishCapabilityUpgradeData(directory, data); err != nil {
		return err
	}
	published, err := readCapabilityFile(root, relative, MaximumCapabilityUpgradeBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(published, data) {
		return fmt.Errorf("capability upgrade record changed during publication")
	}
	_, err = parseCapabilityUpgradeRecord(published, record.Delta.Incoming)
	return err
}

func publishCapabilityUpgradeData(directory *os.Root, data []byte) error {
	if info, err := directory.Lstat("delta.json"); err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("capability upgrade record must be a regular file")
	}
	temporary := ".delta-" + rand.Text() + ".tmp"
	file, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("stage capability upgrade record: %w", err)
	}
	defer directory.Remove(temporary)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	if err := errors.Join(writeErr, syncErr, file.Close()); err != nil {
		return fmt.Errorf("write capability upgrade record: %w", err)
	}
	if err := directory.Rename(temporary, "delta.json"); err != nil {
		return fmt.Errorf("publish capability upgrade record: %w", err)
	}
	return nil
}

func acquireCapabilityUpgradeLock(root *os.Root) (func(), error) {
	file, err := openCapabilityUpgradeLock(root)
	if err != nil {
		return nil, err
	}
	if err := lockCapabilityUpgradeFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("capability upgrade is busy or cannot acquire its write lock: %w", err)
	}
	return func() { _ = file.Close() }, nil
}

func openCapabilityUpgradeLock(root *os.Root) (*os.File, error) {
	const name = ".write-lock"
	created, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if err := created.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := capabilityFileInfo(root, name)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("capability upgrade write lock changed during opening")
	}
	return file, nil
}
