package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inoio/agents-sandbox/internal/atomicwrite"

	"gopkg.in/yaml.v3"
)

// RootState is the launcher-global state document at the top of the state root,
// distinct from per-project state files under <root>/<slug>/<agent>/state.yaml.
//
//exhaustruct:ignore
type RootState struct {
	RuntimeHomeMigration RuntimeHomeMigration `yaml:"runtime_home_migration,omitempty"`
}

// RuntimeHomeMigration records the user's decision about the one-time opt-in
// copy of legacy microsandbox home volumes into the private runtime home.
//
//exhaustruct:ignore
type RuntimeHomeMigration struct {
	Dismissed bool `yaml:"dismissed,omitempty"`
}

func rootStateFile() string {
	return filepath.Join(stateRoot(), "state.yaml")
}

// marshalRootState is a seam over yaml.Marshal so tests can exercise the
// marshal failure path, which the concrete RootState shape cannot trigger.
var marshalRootState = yaml.Marshal

// ReadRootState loads the launcher-global state. A missing file yields a zero
// RootState with no error; parse and other I/O failures are returned.
func ReadRootState() (*RootState, error) {
	path := rootStateFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RootState{}, nil
		}
		return nil, fmt.Errorf("read root state file %s: %w", path, err)
	}
	var state RootState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse root state file %s: %w", path, err)
	}
	return &state, nil
}

// WriteRootState atomically writes the launcher-global state to disk.
func WriteRootState(s RootState) error {
	data, err := marshalRootState(s)
	if err != nil {
		return fmt.Errorf("marshal root state: %w", err)
	}
	if err := atomicwrite.WriteFile(rootStateFile(), data); err != nil {
		return fmt.Errorf("write root state: %w", err)
	}
	return nil
}
