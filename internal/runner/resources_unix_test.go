//go:build unix

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestResourceLockDescriptorClosesAcrossGovernedChildExec(t *testing.T) {
	resource := fmt.Sprintf("close-on-exec-%d", time.Now().UnixNano())
	directory, err := secureResourceLockDirectory()
	if err != nil {
		t.Fatal(err)
	}
	file, err := acquireResourceFile(context.Background(), directory, resource)
	if err != nil {
		t.Fatal(err)
	}
	fdFlags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		_ = file.Close()
		t.Fatalf("read resource lock descriptor flags: %v", errno)
	}
	if fdFlags&syscall.FD_CLOEXEC == 0 {
		_ = file.Close()
		t.Fatal("resource lock descriptor is inherited across exec")
	}

	root := t.TempDir()
	inherited := filepath.Join(root, "inherited")
	script := filepath.Join(root, "governed-child.sh")
	contents := "#!/bin/sh\nif [ -e /dev/fd/$CODE_POLISHY_LOCK_FD ]; then : >\"$CODE_POLISHY_LOCK_INHERITED\"; (sleep 2) & wait; fi\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_LOCK_FD", strconv.FormatUint(uint64(file.Fd()), 10))
	t.Setenv("CODE_POLISHY_LOCK_INHERITED", inherited)
	if err := (OSRunner{}).Run(context.Background(), root, policy.Command{
		Name: "governed-child", Argv: []string{"./governed-child.sh"}, Cwd: ".",
		Environment: []string{"CODE_POLISHY_LOCK_FD", "CODE_POLISHY_LOCK_INHERITED"}, TimeoutSeconds: successfulCommandTimeoutSeconds,
	}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := os.Stat(inherited); err == nil {
		_ = file.Close()
		t.Fatal("governed child or descendant inherited the resource-lock descriptor")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	released, err := acquireResourceFile(ctx, directory, resource)
	if err != nil {
		t.Fatalf("descendant stranded the resource lease after parent release: %v", err)
	}
	if err := released.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateResourceLockRequiresCurrentUserOwnership(t *testing.T) {
	info := resourceLockInfo{mode: 0o600, stat: &syscall.Stat_t{Uid: 1001}}
	if err := validatePrivateResourceLock(info, 1000); err == nil || !strings.Contains(err.Error(), "owned by the current user") {
		t.Fatalf("foreign-owned resource lock was accepted: %v", err)
	}
	if err := validatePrivateResourceLock(resourceLockInfo{mode: 0o600, stat: &syscall.Stat_t{Uid: 1000}}, 1000); err != nil {
		t.Fatalf("current-user resource lock was rejected: %v", err)
	}
}

type resourceLockInfo struct {
	mode os.FileMode
	stat *syscall.Stat_t
}

func (info resourceLockInfo) Name() string       { return "resource.lock" }
func (info resourceLockInfo) Size() int64        { return 0 }
func (info resourceLockInfo) Mode() os.FileMode  { return info.mode }
func (info resourceLockInfo) ModTime() time.Time { return time.Time{} }
func (info resourceLockInfo) IsDir() bool        { return false }
func (info resourceLockInfo) Sys() any           { return info.stat }
