// Package atomicwrite writes launcher state and configuration files atomically.
package atomicwrite

import (
	"fmt"
	"os"
	"path/filepath"
)

// Filesystem seams. Writing to and closing a freshly created temp file only
// fail in situations (e.g. a full disk) that a test filesystem cannot produce,
// so tests substitute them to exercise the error handling.
var (
	writeTempFile = func(f *os.File, data []byte) error {
		_, err := f.Write(data)
		return err
	}
	closeTempFile = (*os.File).Close
)

// WriteFile atomically replaces path with data, creating the parent directory
// (mode 0700) if needed. It writes a uniquely named temporary file in the
// destination directory, then renames it into place: a reader never observes a
// partial file, and two concurrent writers cannot collide on the temporary name
// (the last writer wins). The file is written with mode 0600.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if err := writeTempFile(tmp, data); err != nil {
		_ = closeTempFile(tmp)
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := closeTempFile(tmp); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
