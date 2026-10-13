package runtimemigration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// CopyVolumes copies the named legacy home volumes into newly created volumes
// in the private runtime home. Each volume is a VolumeKindDir created through
// the client (which places it under the private home); its contents are then
// copied host-side as the invoking user. A volume whose copy fails is removed
// best-effort so necessity stays derived true and the offer can retry later.
func CopyVolumes(ctx context.Context, client msb.Client, names []string, ui termio.UI) error {
	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := copyVolume(ctx, client, legacyHome, name); err != nil {
			return err
		}
		ui.Infof("copied legacy home volume %q", name)
	}
	return nil
}

func copyVolume(ctx context.Context, client msb.Client, legacyHome, name string) error {
	volume, err := client.CreateVolume(ctx, name, msbSdk.WithVolumeKind(msbSdk.VolumeKindDir))
	if err != nil {
		return fmt.Errorf("create volume %q: %w", name, err)
	}

	src := filepath.Join(legacyHome, "volumes", name)
	if err := copyTree(src, volume.Path()); err != nil {
		removeVolumeBestEffort(ctx, client, name)
		return fmt.Errorf("copy volume %q: %w", name, err)
	}
	return nil
}

// removeVolumeBestEffort discards a partially copied volume so it does not
// linger as a half-populated volume that necessity would treat as complete.
func removeVolumeBestEffort(ctx context.Context, client msb.Client, name string) {
	_ = client.RemoveVolume(ctx, name)
}

// copyTree recursively copies the tree rooted at src into dst, preserving file
// mode and modification time. Directories are copied depth-first so their
// metadata is applied after their children. Symlinks are recreated as symlinks
// (never followed); non-regular entries (FIFOs, sockets, devices) are skipped.
// src must exist.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}
	if info.IsDir() {
		return copyDir(src, dst, info)
	}
	if !info.Mode().IsRegular() {
		// FIFOs, sockets, and device nodes cannot be meaningfully copied:
		// opening a FIFO blocks forever and a socket fails the copy. Skip them.
		return nil
	}
	return copyFile(src, dst, info)
}

func copyDir(src, dst string, info os.FileInfo) error {
	perm := info.Mode().Perm()
	// Create the destination writable regardless of the source mode, so a
	// read-only source directory can still have its children written. The
	// source mode is restored below, after the children exist.
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}

	// Re-apply dir metadata after writing children, which would otherwise bump
	// the destination directory's mtime.
	if err := os.Chmod(dst, perm); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	perm := info.Mode().Perm()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if err := os.Chmod(dst, perm); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, dst)
}
