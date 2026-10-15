package volume

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

// RemoveVolumes removes the named private home volumes. Unless force is set, it
// refuses when any name is still referenced by a project state file, listing the
// referencing projects, so a project's recorded home is never silently orphaned.
func RemoveVolumes(ctx context.Context, names []string, force bool) error {
	if !force {
		references, err := state.HomeVolumeReferences()
		if err != nil {
			return fmt.Errorf("check volume references: %w", err)
		}
		if err := refuseReferencedVolumes(names, references); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := msb.Get().RemoveVolume(ctx, name); err != nil {
			return fmt.Errorf("remove volume %q: %w", name, err)
		}
	}
	return nil
}

// refuseReferencedVolumes returns an error naming every requested volume that a
// project state file still records, so the caller can decide to force the
// removal explicitly.
func refuseReferencedVolumes(names []string, references map[string][]state.Key) error {
	var refused []string
	for _, name := range names {
		if keys := references[name]; len(keys) > 0 {
			refused = append(refused, fmt.Sprintf("%s (referenced by %s)", name, formatKeys(keys)))
		}
	}
	if len(refused) == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to remove volume(s) still referenced by a project: %s; "+
			"re-run with --force to remove anyway",
		strings.Join(refused, "; "),
	)
}

// formatKeys renders referencing project keys as "slug/agent" (or "slug" for a
// legacy agent-less key), sorted for stable output.
func formatKeys(keys []state.Key) string {
	labels := make([]string, 0, len(keys))
	for _, k := range keys {
		if k.Agent == "" {
			labels = append(labels, k.Slug)
			continue
		}
		labels = append(labels, k.Slug+"/"+k.Agent)
	}
	sort.Strings(labels)
	return strings.Join(labels, ", ")
}
