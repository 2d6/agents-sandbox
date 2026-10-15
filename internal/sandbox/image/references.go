package image

import (
	"slices"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

// InUseImageReferences maps each runner image reference in use to the project
// keys that reference it. It unions two sources: the project-state scan
// (state.ImageReferences), the source of truth for ownership, and the image
// references held by the given kept sandboxes, which catch a digest-pinned VM
// whose tag is no longer the per-agent "-latest" that its slug and agent name
// derive. The project-state scan fails closed: an unreadable or corrupt state
// file is returned as an error rather than treated as unreferenced.
func InUseImageReferences(keptSandboxes []msb.SandboxHandle) (map[string][]state.Key, error) {
	references, err := state.ImageReferences()
	if err != nil {
		return nil, err
	}
	for _, handle := range keptSandboxes {
		ref := handle.Image()
		if ref == "" {
			continue
		}
		key := sandboxKey(handle.Name())
		if key.Slug == "" || slices.Contains(references[ref], key) {
			continue
		}
		references[ref] = append(references[ref], key)
	}
	return references, nil
}

// sandboxKey derives the project key a sandbox belongs to from its name.
func sandboxKey(name string) state.Key {
	info := naming.ArtifactFor(name)
	return state.Key{Slug: info.Slug, Agent: info.Agent}
}
