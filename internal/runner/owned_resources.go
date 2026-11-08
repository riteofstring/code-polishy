package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

var ErrResourceBusy = errors.New("resource is busy")
var ErrUnsafeResource = errors.New("resource path is unsafe")

type ResourceMode uint8

const (
	SharedResourceUse ResourceMode = iota
	ExclusiveResourceUse
	ResourceMaintenance
)

func OwnedResourcePath(key string) string {
	return ".tools/code-polishy-cleanup/" + key
}

func OpenResourceDirectory(root, relative string, create bool) (*os.Root, error) {
	if !filepath.IsLocal(relative) || filepath.ToSlash(filepath.Clean(relative)) != relative || strings.ContainsAny(relative, "\\:") {
		return nil, fmt.Errorf("invalid resource directory %q", relative)
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(relative, "/") {
		child, openErr := openResourceChild(directory, part, create)
		directory.Close()
		if openErr != nil {
			return nil, openErr
		}
		directory = child
	}
	return directory, nil
}

func openResourceChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: directory %q is linked or not a directory", ErrUnsafeResource, name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, fmt.Errorf("%w: directory %q changed while opening", ErrUnsafeResource, name)
	}
	return child, nil
}

func AcquireOwnedResource(ctx context.Context, resource policy.OwnedResource, mode ResourceMode) (*ResourceLease, error) {
	if resource.Root == "" || resource.Key == "" || strings.Trim(resource.Key, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || mode > ResourceMaintenance {
		return nil, errors.New("invalid owned resource declaration")
	}
	directory, err := OpenResourceDirectory(resource.Root, OwnedResourcePath(resource.Key), true)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	name := "live-use"
	if mode == ResourceMaintenance {
		name = "maintenance"
	}
	file, err := openOwnedResourceLock(directory, name)
	if err != nil {
		return nil, err
	}
	files, err := lockOwnedResourceFile(ctx, file, mode == SharedResourceUse, mode != SharedResourceUse)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &ResourceLease{files: files}, nil
}

func openOwnedResourceLock(directory *os.Root, name string) (*os.File, error) {
	info, err := directory.Lstat(name)
	if err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("resource lock %q is not a regular file: %v", name, err)
	}
	file, err := directory.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	named, nameErr := directory.Lstat(name)
	if err != nil || nameErr != nil || !named.Mode().IsRegular() || !os.SameFile(info, named) {
		file.Close()
		return nil, fmt.Errorf("resource lock %q changed while opening", name)
	}
	return file, nil
}

func acquireCommandResources(ctx context.Context, command policy.Command) (*ResourceLease, time.Duration, error) {
	if len(command.OwnedResources) == 0 {
		return AcquireExclusiveResources(ctx, command.ExclusiveResources)
	}
	started := time.Now()
	lease, _, err := AcquireExclusiveResources(ctx, command.ExclusiveResources)
	if err != nil {
		return nil, time.Since(started), err
	}
	leases := resourceLeases{lease}
	resources := slices.Clone(command.OwnedResources)
	slices.SortFunc(resources, func(a, b policy.OwnedResource) int { return strings.Compare(a.Root+"/"+a.Key, b.Root+"/"+b.Key) })
	wait, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, resource := range slices.Compact(resources) {
		owned, err := AcquireOwnedResource(wait, resource, SharedResourceUse)
		if err != nil {
			_ = leases.release()
			return nil, time.Since(started), err
		}
		leases = append(leases, owned)
	}
	return &ResourceLease{files: leases}, time.Since(started), nil
}

type resourceLeases []*ResourceLease

func (leases resourceLeases) release() error {
	var err error
	for index := len(leases) - 1; index >= 0; index-- {
		err = errors.Join(err, leases[index].Release())
	}
	return err
}
