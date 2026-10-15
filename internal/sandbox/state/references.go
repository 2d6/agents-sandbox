package state

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// HomeVolumeReferences maps each home volume name recorded in a project state
// file to the project keys that reference it. It scans both the per-agent layout
// <root>/<slug>/<agent>/state.yaml and the legacy agent-less
// <root>/<slug>/state.yaml, skipping the launcher-global root state file.
//
// A state file that cannot be read or parsed is an error: the caller cannot prove
// the volume is unreferenced, so a destructive removal must fail closed (with an
// explicit override) rather than risk orphaning a project.
func HomeVolumeReferences() (map[string][]Key, error) {
	root := stateRoot()
	references := map[string][]Key{}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "state.yaml" {
			return nil
		}
		key, ok := keyForStateFile(root, path)
		if !ok {
			return nil
		}
		st, readErr := ReadState(key)
		if readErr != nil {
			return readErr
		}
		if st.HomeVolume != "" {
			references[st.HomeVolume] = append(references[st.HomeVolume], key)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan project state references: %w", walkErr)
	}
	return references, nil
}

// keyForStateFile derives the project key for a state.yaml path discovered under
// root. The launcher-global root state file (<root>/state.yaml) is not a project
// state and returns false.
func keyForStateFile(root, path string) (Key, bool) {
	rel, _ := filepath.Rel(root, path)
	parts := strings.Split(rel, string(filepath.Separator))
	switch len(parts) {
	case 2:
		return Key{Slug: parts[0], Agent: ""}, true
	case 3:
		return Key{Slug: parts[0], Agent: parts[1]}, true
	default:
		return Key{}, false
	}
}
