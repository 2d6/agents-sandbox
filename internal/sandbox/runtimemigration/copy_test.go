package runtimemigration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/termio"
)

func TestCopyTreePreservesContentsAndModes(t *testing.T) {
	src := t.TempDir()
	writeFileWithMode(t, filepath.Join(src, "top.txt"), "top", 0o640)
	writeFileWithMode(t, filepath.Join(src, "sub", "nested.txt"), "nested", 0o600)
	if err := os.MkdirAll(filepath.Join(src, "emptydir"), 0o750); err != nil {
		t.Fatalf("create empty dir: %v", err)
	}

	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree() error = %v", err)
	}

	assertFileWithMode(t, filepath.Join(dst, "top.txt"), "top", 0o640)
	assertFileWithMode(t, filepath.Join(dst, "sub", "nested.txt"), "nested", 0o600)

	info, err := os.Stat(filepath.Join(dst, "emptydir"))
	if err != nil {
		t.Fatalf("stat copied empty dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("emptydir is not a directory")
	}
}

func TestCopyTreePreservesModTime(t *testing.T) {
	src := t.TempDir()
	file := filepath.Join(src, "stamped.txt")
	writeFileWithMode(t, file, "x", 0o600)
	want := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(file, want, want); err != nil {
		t.Fatalf("chtimes src: %v", err)
	}

	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "stamped.txt"))
	if err != nil {
		t.Fatalf("stat copied file: %v", err)
	}
	if !info.ModTime().Equal(want) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), want)
	}
}

func TestCopyTreePreservesSymlinks(t *testing.T) {
	src := t.TempDir()
	writeFileWithMode(t, filepath.Join(src, "target.txt"), "x", 0o600)
	if err := os.Symlink("target.txt", filepath.Join(src, "link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree() error = %v", err)
	}

	info, err := os.Lstat(filepath.Join(dst, "link"))
	if err != nil {
		t.Fatalf("lstat copied link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copied link mode = %v, want symlink", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "target.txt" {
		t.Errorf("link target = %q, want %q", target, "target.txt")
	}
}

func TestCopyTreeMissingSourceErrors(t *testing.T) {
	err := copyTree(filepath.Join(t.TempDir(), "absent"), t.TempDir())
	if err == nil {
		t.Fatal("copyTree() error = nil, want error for missing source")
	}
}

// TestCopyTreeCopiesChildrenOfReadOnlyDir pins that a source directory whose
// own mode forbids writing can still have its children copied: the destination
// must be created writable, populated, and only then chmod'd to the source mode.
func TestCopyTreeCopiesChildrenOfReadOnlyDir(t *testing.T) {
	src := t.TempDir()
	readOnly := filepath.Join(src, "readonly")
	if err := os.MkdirAll(readOnly, 0o750); err != nil {
		t.Fatalf("create readonly dir: %v", err)
	}
	writeFileWithMode(t, filepath.Join(readOnly, "child.txt"), "child", 0o600)
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("chmod readonly dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o750) })

	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dst, "readonly"), 0o750) })

	assertFileWithMode(t, filepath.Join(dst, "readonly", "child.txt"), "child", 0o600)
	info, err := os.Stat(filepath.Join(dst, "readonly"))
	if err != nil {
		t.Fatalf("stat copied readonly dir: %v", err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Errorf("copied readonly dir mode = %o, want 0o500", info.Mode().Perm())
	}
}

// TestCopyTreeSkipsSpecialFiles pins that FIFOs and other non-regular entries
// are skipped rather than opened: opening a FIFO blocks forever and a socket
// fails the whole copy. The timeout turns a regression (a hang) into a failure.
func TestCopyTreeSkipsSpecialFiles(t *testing.T) {
	src := t.TempDir()
	writeFileWithMode(t, filepath.Join(src, "regular.txt"), "regular", 0o600)

	fifo := filepath.Join(src, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a FIFO on this platform: %v", err)
	}

	dst := t.TempDir()
	done := make(chan error, 1)
	go func() { done <- copyTree(src, dst) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("copyTree() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copyTree() hung on a special file")
	}

	assertFileWithMode(t, filepath.Join(dst, "regular.txt"), "regular", 0o600)
	if _, err := os.Lstat(filepath.Join(dst, "pipe")); !os.IsNotExist(err) {
		t.Errorf("special file was copied (lstat err = %v), want it skipped", err)
	}
}

func TestCopyVolumesCopiesIntoCreatedVolume(t *testing.T) {
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "legacy"
	addVolumeDir(t, legacy, name)
	writeFileWithMode(t, filepath.Join(legacy, "volumes", name, "config", "settings.json"), "{}", 0o600)

	target := t.TempDir()
	var created []string
	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, volumeName string, opts ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			created = append(created, volumeName)
			return &msb.MockVolumeHandle{Name_: volumeName, Path_: target}, nil
		},
	}

	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, []string{name}, &ui); err != nil {
		t.Fatalf("CopyVolumes() error = %v", err)
	}

	if len(created) != 1 || created[0] != name {
		t.Fatalf("CreateVolume calls = %v, want [%q]", created, name)
	}
	assertFileWithMode(t, filepath.Join(target, "config", "settings.json"), "{}", 0o600)
}

func TestCopyVolumesCreateErrorPropagates(t *testing.T) {
	installLegacyHome(t)
	name := naming.HomePrefix + "legacy"

	wantErr := errors.New("boom")
	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, _ string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			return nil, wantErr
		},
	}

	ui := termio.NewTestMock(t)
	err := CopyVolumes(context.Background(), client, []string{name}, &ui)
	if !errors.Is(err, wantErr) {
		t.Fatalf("CopyVolumes() error = %v, want %v", err, wantErr)
	}
}

func TestCopyVolumesRemovesVolumeOnCopyFailure(t *testing.T) {
	installLegacyHome(t)
	name := naming.HomePrefix + "legacy"

	target := t.TempDir()
	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, volumeName string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			return &msb.MockVolumeHandle{Name_: volumeName, Path_: target}, nil
		},
	}

	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, []string{name}, &ui); err == nil {
		t.Fatal("CopyVolumes() error = nil, want error for missing legacy volume dir")
	}
	if len(client.RemovedVolumes) != 1 || client.RemovedVolumes[0] != name {
		t.Fatalf("RemovedVolumes = %v, want [%q] (partial copy cleaned up)", client.RemovedVolumes, name)
	}
}

func writeFileWithMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create parent dir for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %q: %v", path, err)
	}
}

func assertFileWithMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(got) != content {
		t.Errorf("%q content = %q, want %q", path, got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if info.Mode().Perm() != mode {
		t.Errorf("%q mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
}
