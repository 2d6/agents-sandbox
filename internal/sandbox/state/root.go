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
	Migrations map[string]Migration `yaml:"migrations,omitempty"`
}

// MigrationStatus is the terminal or in-flight state of a one-time migration.
// The absent entry (no key) is the pending state and has no status value.
type MigrationStatus string

const (
	MigrationCopying   MigrationStatus = "copying"
	MigrationCompleted MigrationStatus = "completed"
	MigrationDismissed MigrationStatus = "dismissed"
)

// Migration tracks one one-time migration. Copied, Kept, and Excluded are
// written only while Status is MigrationCopying and dropped when the migration
// reaches a terminal status.
//
// The three lists are disjoint and together account for every legacy volume the
// campaign has considered:
//   - Copied: fully migrated into the private home.
//   - Kept: dismissed from the migration at the user's choice (option "copy the
//     missing volumes"); the present private volume is intentionally left as-is.
//   - Excluded: dropped from the migration by a copy failure; the partial
//     destination has been removed.
//
//exhaustruct:ignore
type Migration struct {
	Status   MigrationStatus `yaml:"status"`
	Copied   []string        `yaml:"copied,omitempty"`
	Kept     []string        `yaml:"kept,omitempty"`
	Excluded []string        `yaml:"excluded,omitempty"`
}

// Migration returns the named migration and whether it is present. A missing
// registry or entry is the pending state: the zero Migration.
func (s *RootState) Migration(name string) (Migration, bool) {
	m, ok := s.Migrations[name]
	return m, ok
}

// SetMigration records a migration entry, initializing the registry if nil.
func (s *RootState) SetMigration(name string, m Migration) {
	if s.Migrations == nil {
		s.Migrations = map[string]Migration{}
	}
	s.Migrations[name] = m
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

// UpdateRootState reads the launcher-global state (a missing file yields a zero
// RootState), applies mutate, and writes it back atomically. It is a
// read-modify-write so a writer preserves entries it does not touch.
func UpdateRootState(mutate func(*RootState)) error {
	s, err := ReadRootState()
	if err != nil {
		return err
	}
	mutate(s)
	return WriteRootState(*s)
}
