package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// runtimeSubtrees are the private-home subdirectories that hold the
// runtime/FFI artifacts agents-sandbox installs: the msb binary and libkrunfw.
// The rest of the private home is microsandbox-owned and reported by
// microsandbox's storage observation, so it is not walked here.
var runtimeSubtrees = []string{"bin", "lib"}

// observeRuntime sums the regular files under the private home's runtime
// subtrees. The count and logical bytes are known; allocated bytes and
// reclaimable bytes are not observed. A missing subtree is a known zero, while
// any other walk error propagates.
func observeRuntime(home string, opts Options) (Category, error) {
	var accumulator runtimeAccumulator
	for _, sub := range runtimeSubtrees {
		if err := accumulator.scan(filepath.Join(home, sub), opts.IncludeObjects); err != nil {
			return Category{}, err
		}
	}
	return accumulator.category(), nil
}

// runtimeAccumulator sums regular files and, on request, collects one object per
// file while walking the runtime subtrees.
type runtimeAccumulator struct {
	count        uint64
	logicalBytes uint64
	objects      []Object
}

func (a *runtimeAccumulator) scan(root string, includeObjects bool) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		return a.visit(path, entry, err, includeObjects)
	})
	if err != nil {
		return fmt.Errorf("scan runtime artifacts in %s: %w", root, err)
	}
	return nil
}

func (a *runtimeAccumulator) visit(path string, entry fs.DirEntry, err error, includeObjects bool) error {
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fs.SkipDir
		}
		return err
	}
	if entry.IsDir() {
		return nil
	}
	info, infoErr := entry.Info()
	if infoErr != nil {
		return infoErr
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	size := uint64(info.Size()) //nolint:gosec // regular-file sizes are non-negative
	a.count++
	a.logicalBytes += size
	if includeObjects {
		a.objects = append(a.objects, Object{Name: filepath.Base(path), Path: path, LogicalBytes: &size})
	}
	return nil
}

func (a *runtimeAccumulator) category() Category {
	count, logicalBytes := a.count, a.logicalBytes
	return Category{
		Name:    CategoryRuntime,
		Totals:  Totals{Count: &count, LogicalBytes: &logicalBytes},
		Objects: a.objects,
	}
}
