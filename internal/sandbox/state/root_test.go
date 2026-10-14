package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/testutil"
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

// TestReadRootState_NonNotFoundReadError covers a read failure that is not
// "not exist": a directory at the state file path.
func TestReadRootState_NonNotFoundReadError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	if err := os.MkdirAll(rootStateFile(), 0o700); err != nil {
		t.Fatalf("mkdir at state file path: %v", err)
	}

	if _, err := ReadRootState(); err == nil {
		t.Fatal("ReadRootState() error = nil, want a non-not-exist read error")
	}
}

// TestWriteRootState_MarshalError covers the marshal failure via the seam: the
// concrete RootState shape cannot make yaml.Marshal fail.
func TestWriteRootState_MarshalError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	wantErr := errors.New("marshal boom")
	orig := marshalRootState
	marshalRootState = func(any) ([]byte, error) { return nil, wantErr }
	t.Cleanup(func() { marshalRootState = orig })

	if err := WriteRootState(
		RootState{RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true}},
	); !errors.Is(
		err,
		wantErr,
	) {
		t.Fatalf("WriteRootState() error = %v, want %v", err, wantErr)
	}
}

// TestWriteRootState_MkdirAllError covers the state-root creation failure: a
// regular file occupies the directory's path.
func TestWriteRootState_MkdirAllError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	root := stateRoot()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	testutil.WritePath(t, root, "not a directory")

	if err := WriteRootState(RootState{RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true}}); err == nil {
		t.Fatal("WriteRootState() error = nil, want MkdirAll failure")
	}
}

// TestWriteRootState_WriteTempError covers the temp-file write failure: a
// directory occupies the temp file's path.
func TestWriteRootState_WriteTempError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	if err := os.MkdirAll(filepath.Join(stateRoot(), ".state.yaml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := WriteRootState(RootState{RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true}}); err == nil {
		t.Fatal("WriteRootState() error = nil, want temp write failure")
	}
}

// TestWriteRootState_RenameErrorCleansTemp covers the rename failure: a
// directory occupies the final state file path. The temp file must be cleaned.
func TestWriteRootState_RenameErrorCleansTemp(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	if err := os.MkdirAll(rootStateFile(), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := WriteRootState(RootState{RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true}}); err == nil {
		t.Fatal("WriteRootState() error = nil, want rename failure")
	}
	if _, err := os.Stat(filepath.Join(stateRoot(), ".state.yaml.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed after failed rename, got %v", err)
	}
}
