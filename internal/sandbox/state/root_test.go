package state

import (
	"path/filepath"
	"testing"

	"github.com/inoio/agents-sandbox/internal/configpaths"
)

func TestRootStateFile(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	f := rootStateFile()
	if filepath.Base(f) != "state.yaml" {
		t.Errorf("expected state.yaml basename, got %q", filepath.Base(f))
	}
	if filepath.Dir(f) != stateRoot() {
		t.Errorf("expected root state under %q, got %q", stateRoot(), f)
	}
}

func TestReadRootState_MissingFileReturnsZero(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	result, err := ReadRootState()
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil zero RootState for missing file")
	}
	if result.RuntimeHomeMigration.Dismissed {
		t.Error("expected Dismissed false for missing file")
	}
}

func TestWriteAndReadRootState_RoundTrip(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	err := WriteRootState(RootState{
		RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true},
	})
	if err != nil {
		t.Fatalf("WriteRootState: %v", err)
	}

	result, err := ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if !result.RuntimeHomeMigration.Dismissed {
		t.Errorf("Dismissed = %v, want true", result.RuntimeHomeMigration.Dismissed)
	}
}
