// Package runtimemigration derives and, in later stages, performs the opt-in
// copy of legacy microsandbox home volumes into the private runtime home.
package runtimemigration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

// legacyVolumeNames returns the basenames of agents-sandbox home volumes under
// <legacyHome>/volumes. A volumes directory that is missing, not a directory, or
// unreadable (permission denied) yields no names and no error: there is simply
// nothing usable to copy, so necessity is false rather than a failure.
func legacyVolumeNames(legacyHome string) ([]string, error) {
	return homeVolumeNames(legacyHome)
}

// presentPrivateVolumeNames returns the basenames of agents-sandbox home volumes
// present under <privateHome>/volumes, with the same missing/unreadable
// tolerance as legacyVolumeNames. Presence alone is not completion: whether a
// present volume counts as copied is a status question the caller (see
// necessaryVolumes) answers from the migration registry, not from the volume.
func presentPrivateVolumeNames(privateHome string) ([]string, error) {
	return homeVolumeNames(privateHome)
}

// necessaryVolumes returns the legacy home volume names that still need copying,
// given the current migration status. The migration is the source of truth for
// what "necessary" means:
//   - completed/dismissed: the campaign is over; nothing is ever necessary.
//   - copying: legacy names minus those already Copied and those Excluded. The
//     private listing is not consulted, so a present volume not recorded as
//     Copied is an unambiguous partial and stays necessary (to be re-copied).
//   - pending (zero status): legacy names absent from the private home.
func necessaryVolumes(legacyHome, privateHome string, m state.Migration) ([]string, error) {
	switch m.Status {
	case state.MigrationCompleted, state.MigrationDismissed:
		return nil, nil
	case state.MigrationCopying:
		return copyingNecessaryVolumes(legacyHome, m)
	default:
		return pendingNecessaryVolumes(legacyHome, privateHome)
	}
}

// pendingNecessaryVolumes returns the legacy volume names absent from the
// private home. No in-flight progress is recorded yet, so presence is the only
// signal.
func pendingNecessaryVolumes(legacyHome, privateHome string) ([]string, error) {
	legacy, err := legacyVolumeNames(legacyHome)
	if err != nil {
		return nil, err
	}
	private, err := presentPrivateVolumeNames(privateHome)
	if err != nil {
		return nil, err
	}

	present := make(map[string]bool, len(private))
	for _, name := range private {
		present[name] = true
	}

	necessary := make([]string, 0, len(legacy))
	for _, name := range legacy {
		if !present[name] {
			necessary = append(necessary, name)
		}
	}
	return necessary, nil
}

// copyingNecessaryVolumes returns the legacy volume names not yet accounted for
// by an open campaign: those neither Copied nor Excluded. It deliberately
// ignores the private listing, because during a campaign a present-but-unrecorded
// private volume is a partial that must be re-copied.
func copyingNecessaryVolumes(legacyHome string, m state.Migration) ([]string, error) {
	legacy, err := legacyVolumeNames(legacyHome)
	if err != nil {
		return nil, err
	}

	accounted := make(map[string]bool, len(m.Copied)+len(m.Excluded))
	for _, name := range m.Copied {
		accounted[name] = true
	}
	for _, name := range m.Excluded {
		accounted[name] = true
	}

	necessary := make([]string, 0, len(legacy))
	for _, name := range legacy {
		if !accounted[name] {
			necessary = append(necessary, name)
		}
	}
	return necessary, nil
}

// Necessary reports whether any legacy home volume needs copying into the
// private home. Homes and the migration status are resolved via the runtime and
// state helpers.
func Necessary() (bool, error) {
	names, err := NecessaryVolumes()
	if err != nil {
		return false, err
	}
	return len(names) > 0, nil
}

// NecessaryVolumes returns the legacy home volumes that need copying, given the
// persisted migration status. Both homes are resolved via the runtime helpers;
// the migration entry is read from root state (an absent entry is the pending
// state).
func NecessaryVolumes() ([]string, error) {
	root, err := state.ReadRootState()
	if err != nil {
		return nil, err
	}
	m, _ := root.Migration(MigrationName)
	return necessaryVolumesFor(m)
}

// necessaryVolumesFor resolves both homes and delegates to necessaryVolumes.
func necessaryVolumesFor(m state.Migration) ([]string, error) {
	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return nil, err
	}
	return necessaryVolumes(legacyHome, runtime.PrivateHome(), m)
}

func homeVolumeNames(home string) ([]string, error) {
	volumesDir := filepath.Join(home, "volumes")
	entries, err := os.ReadDir(volumesDir)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrPermission) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if name := entry.Name(); strings.HasPrefix(name, naming.HomePrefix) {
			names = append(names, name)
		}
	}
	return names, nil
}
