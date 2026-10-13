package state

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

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

// TestWriteRootStateUsesDocumentedWireKeys pins the on-disk YAML contract: the
// single launcher-global key is runtime_home_migration, and its inner key is
// dismissed. These names are the persisted interface other sessions rely on.
func TestWriteRootStateUsesDocumentedWireKeys(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	if err := WriteRootState(RootState{
		RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true},
	}); err != nil {
		t.Fatalf("WriteRootState: %v", err)
	}

	data, err := os.ReadFile(rootStateFile())
	if err != nil {
		t.Fatalf("read root state file: %v", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw root state: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("top-level keys = %v, want exactly [runtime_home_migration]", raw)
	}
	inner, ok := raw["runtime_home_migration"].(map[string]any)
	if !ok {
		t.Fatalf("runtime_home_migration = %v, want a mapping", raw["runtime_home_migration"])
	}
	if len(inner) != 1 {
		t.Fatalf("inner keys = %v, want exactly [dismissed]", inner)
	}
	if inner["dismissed"] != true {
		t.Errorf("dismissed = %v, want true", inner["dismissed"])
	}
}

func TestReadRootState_ParseError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	if err := os.MkdirAll(stateRoot(), 0o700); err != nil {
		t.Fatalf("create state root: %v", err)
	}
	if err := os.WriteFile(rootStateFile(), []byte("runtime_home_migration: [unterminated"), 0o600); err != nil {
		t.Fatalf("write corrupt root state: %v", err)
	}

	if _, err := ReadRootState(); err == nil {
		t.Fatal("ReadRootState() error = nil, want a parse error")
	}
}
