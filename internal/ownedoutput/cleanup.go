package ownedoutput

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type Resource struct {
	Use  policy.OwnedResource
	Path string
}

var errSpecialEntry = errors.New("cache contains a special entry")

type Entry struct {
	Key, Path, Action, Reason, Detail, PendingPath string
	Bytes                                          int64
	SizeKnown                                      bool
}

type disposal struct {
	resource Resource
	root     *os.Root
	dryRun   bool
	entries  []Entry
	problem  error
}

func Clean(ctx context.Context, resource Resource, dryRun bool) ([]Entry, error) {
	if resource.Path == "." || resource.Use.Root == "" {
		return nil, errors.New("owned output must be below a checkout root")
	}
	canonical, err := filepath.EvalSymlinks(resource.Use.Root)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	resource.Use.Root = canonical
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	d := disposal{resource: resource, root: root, dryRun: dryRun}
	d.run(ctx)
	return d.entries, d.problem
}

func (d *disposal) record(path, action, reason string, err error) {
	entry := Entry{Key: d.resource.Use.Key, Path: path, Action: action, Reason: reason}
	if err != nil {
		entry.Detail = err.Error()
	}
	if path != d.resource.Path {
		entry.PendingPath = path
	}
	d.entries = append(d.entries, entry)
	if action == "error" {
		d.problem = errors.Join(d.problem, fmt.Errorf("%s: %w", path, err))
	}
}

func (d *disposal) probe(path string) bool {
	dir, err := runner.OpenResourceDirectory(d.resource.Use.Root, path, false)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		action := "error"
		if errors.Is(err, runner.ErrUnsafeResource) {
			action = "kept"
		}
		d.record(path, action, "unsafe-layout", err)
		return false
	}
	dir.Close()
	return true
}

func (d *disposal) run(ctx context.Context) {
	control := runner.OwnedResourcePath(d.resource.Use.Key)
	pending := control + "/removals"
	live, staged := d.probe(d.resource.Path), d.probe(pending)
	if !live && !staged {
		return
	}
	if err := mountGuard(d.resource.Use.Root, control, true); err != nil {
		d.record(control, "kept", "unsupported-layout", err)
		return
	}
	if !d.dryRun {
		maintenance, err := runner.AcquireOwnedResource(ctx, d.resource.Use, runner.ResourceMaintenance)
		if err != nil {
			d.lockFailure(control, "maintenance-busy", err)
			return
		}
		defer func() { d.problem = errors.Join(d.problem, maintenance.Release()) }()
	}
	if staged {
		d.pending(ctx, pending)
	}
	if err := ctx.Err(); err != nil {
		d.record(d.resource.Path, "error", "canceled", err)
		return
	}
	if live {
		if d.dryRun {
			d.remove(ctx, d.resource.Path)
		} else {
			d.detach(ctx, pending)
		}
	}
}

func (d *disposal) lockFailure(path, busy string, err error) {
	if errors.Is(err, runner.ErrResourceBusy) {
		d.record(path, "busy", busy, err)
	} else {
		d.record(path, "error", "lock-failed", err)
	}
}

func (d *disposal) pending(ctx context.Context, path string) {
	if err := mountGuard(d.resource.Use.Root, path, true); err != nil {
		d.record(path, "kept", "unsafe-layout", err)
		return
	}
	entries, err := fs.ReadDir(d.root.FS(), path)
	if err != nil {
		d.record(path, "error", "read-pending", err)
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		slot := path + "/" + entry.Name()
		if !d.probe(slot) {
			continue
		}
		d.remove(ctx, slot)
	}
}

func (d *disposal) detach(ctx context.Context, pending string) {
	lease, err := runner.AcquireOwnedResource(ctx, d.resource.Use, runner.ExclusiveResourceUse)
	if err != nil {
		d.lockFailure(d.resource.Path, "active", err)
		return
	}
	path, err := d.move(pending)
	err = errors.Join(err, lease.Release())
	if err != nil {
		d.record(d.resource.Path, "error", "detach-failed", err)
		return
	}
	if path != "" {
		before := len(d.entries)
		d.remove(ctx, path)
		if len(d.entries) > before {
			entry := &d.entries[len(d.entries)-1]
			entry.Path = d.resource.Path
			if entry.Action != "removed" {
				entry.PendingPath = path
				entry.Detail += "; live cache detached; the next build recreates it"
			}
		}
	}
}

func (d *disposal) move(pending string) (string, error) {
	if !d.probe(d.resource.Path) {
		return "", nil
	}
	if err := mountGuard(d.resource.Use.Root, d.resource.Path, false); err != nil {
		d.record(d.resource.Path, "kept", "unsupported-layout", err)
		return "", nil
	}
	directory, err := runner.OpenResourceDirectory(d.resource.Use.Root, pending, true)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	slot, err := os.MkdirTemp(directory.Name(), "remove-")
	if err != nil {
		return "", err
	}
	relative := pending + "/" + filepath.Base(slot)
	if err := d.root.Rename(d.resource.Path, relative+"/cache"); err != nil {
		_ = d.root.Remove(relative)
		if errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EBUSY) {
			d.record(d.resource.Path, "kept", "cross-device", err)
			return "", nil
		}
		return "", err
	}
	return relative, nil
}

func (d *disposal) remove(ctx context.Context, path string) {
	if err := mountGuard(d.resource.Use.Root, path, true); err != nil {
		d.record(path, "kept", "unsafe-layout", err)
		return
	}
	bytes, err := treeBytes(ctx, d.root, path)
	if err != nil {
		if d.dryRun && errors.Is(err, os.ErrNotExist) {
			d.record(path, "would-remove", "estimate-unavailable", err)
		} else if errors.Is(err, errSpecialEntry) {
			d.record(path, "kept", "unsafe-layout", err)
		} else {
			d.record(path, "error", "measurement-failed", err)
		}
		return
	}
	action := "would-remove"
	if !d.dryRun {
		if err := ctx.Err(); err != nil {
			d.record(path, "error", "canceled", err)
			return
		}
		if err := d.root.RemoveAll(path); err != nil {
			d.record(path, "error", "removal-failed", err)
			return
		}
		action = "removed"
	}
	d.entries = append(d.entries, Entry{Key: d.resource.Use.Key, Path: path, Action: action, Bytes: bytes, SizeKnown: true})
}

func treeBytes(ctx context.Context, root *os.Root, path string) (int64, error) {
	var bytes int64
	err := fs.WalkDir(root.FS(), path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		} else if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return errSpecialEntry
		}
		return nil
	})
	return bytes, err
}
