package runtimemigration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
)

func TestAcquireMigrationLockBlocksSecondHolder(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	release, err := acquireMigrationLock()
	if err != nil {
		t.Fatalf("acquireMigrationLock: %v", err)
	}
	done := make(chan struct{})
	go func() {
		r, _ := acquireMigrationLock()
		if r != nil {
			r()
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second acquire returned while the first held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
}

// TestAcquireMigrationLockOpenError covers the OpenFile failure: a directory
// occupying the lock file path makes the open fail with EISDIR.
func TestAcquireMigrationLockOpenError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	lockPath := filepath.Join(runtime.PrivateHome(), ".agents-sandbox", "migration.lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatalf("mkdir at lock path: %v", err)
	}

	if _, err := acquireMigrationLock(); err == nil {
		t.Fatal("acquireMigrationLock() error = nil, want an open failure")
	}
}

// TestAcquireMigrationLockFlockError covers the FlockExclusive failure via the
// seam, which a real file cannot trigger.
func TestAcquireMigrationLockFlockError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	wantErr := errors.New("flock boom")
	orig := flockExclusive
	flockExclusive = func(*os.File) error { return wantErr }
	t.Cleanup(func() { flockExclusive = orig })

	if _, err := acquireMigrationLock(); !errors.Is(err, wantErr) {
		t.Fatalf("acquireMigrationLock() error = %v, want %v", err, wantErr)
	}
}
