package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func TestObserveRuntimeSumsBinAndLib(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "bin", "msb"), 100)
	writeFile(t, filepath.Join(home, "lib", "libkrunfw.so.5.6.1"), 200)

	category, err := observeRuntime(home, Options{})
	if err != nil {
		t.Fatalf("observeRuntime() error = %v", err)
	}
	if category.Name != CategoryRuntime {
		t.Errorf("name = %s, want %s", category.Name, CategoryRuntime)
	}
	if got := category.Totals.Count; got == nil || *got != 2 {
		t.Errorf("count = %v, want 2", got)
	}
	if got := category.Totals.LogicalBytes; got == nil || *got != 300 {
		t.Errorf("logical bytes = %v, want 300", got)
	}
	if category.Totals.AllocatedBytes != nil || category.Totals.ReclaimableBytes != nil {
		t.Errorf("allocated/reclaimable = %v/%v, want nil (not observed)",
			category.Totals.AllocatedBytes, category.Totals.ReclaimableBytes)
	}
	if category.Objects != nil {
		t.Errorf("objects = %v, want nil (not requested)", category.Objects)
	}
}

func TestObserveRuntimeMissingHomeIsKnownZero(t *testing.T) {
	category, err := observeRuntime(t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("observeRuntime() error = %v", err)
	}
	if got := category.Totals.Count; got == nil || *got != 0 {
		t.Errorf("count = %v, want known zero", got)
	}
	if got := category.Totals.LogicalBytes; got == nil || *got != 0 {
		t.Errorf("logical bytes = %v, want known zero", got)
	}
}

func TestObserveRuntimeWalkErrorPropagates(t *testing.T) {
	home := t.TempDir()
	locked := filepath.Join(home, "bin", "locked")
	writeFile(t, filepath.Join(locked, "msb"), 10)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := observeRuntime(home, Options{}); err == nil {
		t.Fatal("observeRuntime() error = nil, want propagation of unreadable subtree")
	}
}

func TestObserveRuntimeSkipsNonRegularEntries(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "bin", "msb"), 32)
	if err := os.Symlink(filepath.Join(home, "bin", "msb"), filepath.Join(home, "bin", "link")); err != nil {
		t.Fatalf("Symlink error = %v", err)
	}

	category, err := observeRuntime(home, Options{})
	if err != nil {
		t.Fatalf("observeRuntime() error = %v", err)
	}
	if got := category.Totals.Count; got == nil || *got != 1 {
		t.Errorf("count = %v, want 1 (symlink skipped)", got)
	}
}
