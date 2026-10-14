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
)

// legacyVolumeNames returns the basenames of agents-sandbox home volumes under
// <legacyHome>/volumes. A volumes directory that is missing, not a directory, or
// unreadable (permission denied) yields no names and no error: there is simply
// nothing usable to copy, so necessity is false rather than a failure.
func legacyVolumeNames(legacyHome string) ([]string, error) {
	return homeVolumeNames(legacyHome)
}

// privateVolumeNames returns the basenames of completed agents-sandbox home
// volumes under <privateHome>/volumes, with the same missing/unreadable
// tolerance as legacyVolumeNames. A volume whose copy was interrupted (it lacks
// the completion marker) is excluded, so necessity treats it as still-to-copy.
func privateVolumeNames(privateHome string) ([]string, error) {
	names, err := homeVolumeNames(privateHome)
	if err != nil {
		return nil, err
	}

	completed := make([]string, 0, len(names))
	for _, name := range names {
		if hasCopyCompleteMarker(privateHome, name) {
			completed = append(completed, name)
		}
	}
	return completed, nil
}

// hasCopyCompleteMarker reports whether the named private volume carries the
// copy completion marker, i.e. a finished copy rather than an interrupted one.
func hasCopyCompleteMarker(privateHome, name string) bool {
	marker := filepath.Join(privateHome, "volumes", name, copyCompleteMarker)
	_, err := os.Stat(marker)
	return err == nil
}

// necessaryVolumes returns the legacy home volume names that are absent from the
// private home: the set the opt-in copy would transfer. Necessity is derived
// purely from the two directory listings; nothing is persisted for it.
func necessaryVolumes(legacyHome, privateHome string) ([]string, error) {
	legacy, err := legacyVolumeNames(legacyHome)
	if err != nil {
		return nil, err
	}
	private, err := privateVolumeNames(privateHome)
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

// Necessary reports whether any legacy home volume needs copying into the
// private home. Homes are resolved via the runtime helpers.
func Necessary() (bool, error) {
	names, err := NecessaryVolumes()
	if err != nil {
		return false, err
	}
	return len(names) > 0, nil
}

// NecessaryVolumes returns the legacy home volumes missing from the private
// home, resolving both homes via the runtime helpers.
func NecessaryVolumes() ([]string, error) {
	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return nil, err
	}
	return necessaryVolumes(legacyHome, runtime.PrivateHome())
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
