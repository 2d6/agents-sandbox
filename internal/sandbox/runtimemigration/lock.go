package runtimemigration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

// acquireMigrationLock takes the host-only advisory lock that serializes the
// legacy home-volume copy campaign across processes. The flock auto-releases on
// process exit, so a crashed holder never pins the campaign. The returned
// function releases the lock.
func acquireMigrationLock() (func(), error) {
	dir := filepath.Join(runtime.PrivateHome(), ".agents-sandbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create migration lock dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "migration.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open migration lock: %w", err)
	}
	if err := state.FlockExclusive(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock migration lock: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
