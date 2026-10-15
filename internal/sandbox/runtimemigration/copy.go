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
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// Filesystem seams. A real filesystem makes some failure branches (chmod, copy,
// close, readlink) impractical to trigger, so tests substitute them to exercise
// the error handling.
var (
	ioCopy    = io.Copy
	chmodPath = os.Chmod
	closeFile = (*os.File).Close
	readLink  = os.Readlink
)

// saveMigration persists a migration entry via a read-modify-write so other
// registry entries survive.
func saveMigration(m state.Migration) error {
	return state.UpdateRootState(func(root *state.RootState) {
		root.SetMigration(MigrationName, m)
	})
}

// CopyVolumes runs the legacy home-volume copy campaign under a host-only lock,
// recording progress in the migration registry so an interrupted campaign
// resumes exactly and a concurrent session cannot clobber a live copy.
//
// A terminal migration (completed or dismissed) is a no-op. Starting from
// pending records the legacy targets already present in the private home as Kept
// (dismissed from the migration at the user's choice), so accepting the offer
// neither discards nor re-copies a home the user already has. A volume-scoped
// copy failure is recorded in Excluded (with a warning) and the campaign
// continues; a campaign-scoped failure (the lock, the state read/write, or the
// home listing) returns the error and leaves the status copying so the next
// session resumes.
func CopyVolumes(ctx context.Context, client msb.Client, ui termio.UI) error {
	release, err := acquireMigrationLock()
	if err != nil {
		return err
	}
	defer release()

	root, err := state.ReadRootState()
	if err != nil {
		return err
	}
	m, _ := root.Migration(MigrationName)
	if m.Status == state.MigrationCompleted || m.Status == state.MigrationDismissed {
		return nil
	}

	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return err
	}
	if m.Status != state.MigrationCopying {
		m, err = startCampaign(legacyHome)
		if err != nil {
			return err
		}
	}

	names, err := necessaryVolumesFor(m)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := copyVolume(ctx, client, legacyHome, name); err != nil {
			removeVolumeBestEffort(ctx, client, name)
			m.Excluded = append(m.Excluded, name)
			if serr := saveMigration(m); serr != nil {
				return serr
			}
			ui.Warnf("skipping legacy home volume %q: %v", name, err)
			continue
		}
		m.Copied = append(m.Copied, name)
		if err := saveMigration(m); err != nil {
			return err
		}
		ui.Infof("copied legacy home volume %q", name)
	}
	return saveMigration(state.Migration{Status: state.MigrationCompleted})
}

// startCampaign opens the migration campaign, recording the legacy targets
// already present in the private home as Kept and persisting the copying status,
// so an interrupted campaign resumes without re-asking for them.
func startCampaign(legacyHome string) (state.Migration, error) {
	present, err := presentLegacyTargets(legacyHome, runtime.PrivateHome())
	if err != nil {
		return state.Migration{}, err
	}
	m := state.Migration{Status: state.MigrationCopying, Kept: present}
	return m, saveMigration(m)
}

// copyVolume discards any leftover partial volume, creates the destination
// volume, and copies the legacy tree into it host-side. Completion is recorded
// by the caller in the host-owned migration registry, never as a file inside
// the guest-writable volume.
func copyVolume(ctx context.Context, client msb.Client, legacyHome, name string) error {
	if err := discardIncompleteVolume(ctx, client, name); err != nil {
		return err
	}

	volume, err := client.CreateVolume(ctx, name, msbSdk.WithVolumeKind(msbSdk.VolumeKindDir))
	if err != nil {
		return fmt.Errorf("create volume %q: %w", name, err)
	}

	src := filepath.Join(legacyHome, "volumes", name)
	if err := copyTree(src, volume.Path()); err != nil {
		return fmt.Errorf("copy volume %q: %w", name, err)
	}
	return nil
}

// discardIncompleteVolume removes a leftover private volume under the target
// name before a (re)copy. Such a volume is always an interrupted prior copy — a
// campaign only re-copies names not recorded as Copied — so it must be discarded
// for CreateVolume to recreate it.
func discardIncompleteVolume(ctx context.Context, client msb.Client, name string) error {
	if _, err := client.GetVolume(ctx, name); err != nil {
		return nil //nolint:nilerr // a failed lookup means there is nothing to discard
	}
	if err := client.RemoveVolume(ctx, name); err != nil {
		return fmt.Errorf("discard incomplete volume %q: %w", name, err)
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
	if err := chmodPath(dst, perm); err != nil {
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

	if _, err := ioCopy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := closeFile(out); err != nil {
		return err
	}

	if err := chmodPath(dst, perm); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func copySymlink(src, dst string) error {
	target, err := readLink(src)
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
