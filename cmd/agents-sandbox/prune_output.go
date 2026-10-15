package main

import (
	"encoding/json"

	"github.com/inoio/agents-sandbox/internal/sandbox/pruning"
	"github.com/inoio/agents-sandbox/internal/termio"
)

type pruneDetailJSON struct {
	Type            string `json:"type"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Digest          string `json:"digest"`
	StaleForSeconds int64  `json:"stale_for_seconds"`
}

type pruneReportJSON struct {
	DryRun       bool              `json:"dry_run"`
	Sandboxes    int               `json:"sandboxes"`
	Volumes      int               `json:"volumes"`
	Images       int               `json:"images"`
	DockerImages int               `json:"docker_images"`
	Details      []pruneDetailJSON `json:"details"`
}

type imageReportJSON struct {
	DryRun       bool              `json:"dry_run"`
	Images       int               `json:"images"`
	DockerImages int               `json:"docker_images"`
	Details      []pruneDetailJSON `json:"details"`
}

type volumeReportJSON struct {
	DryRun  bool              `json:"dry_run"`
	Volumes int               `json:"volumes"`
	Details []pruneDetailJSON `json:"details"`
}

type sandboxReportJSON struct {
	DryRun    bool              `json:"dry_run"`
	Sandboxes int               `json:"sandboxes"`
	Details   []pruneDetailJSON `json:"details"`
}

func newPruneReportJSON(report *pruning.StaleReport, dryRun bool) pruneReportJSON {
	return pruneReportJSON{
		DryRun:       dryRun,
		Sandboxes:    report.PrunedSandboxes,
		Volumes:      report.PrunedVolumes,
		Images:       report.PrunedSandboxImages,
		DockerImages: report.PrunedDockerImages,
		Details:      newPruneDetailsJSON(report.Details),
	}
}

func newImageReportJSON(report pruning.ImageReport, dryRun bool) imageReportJSON {
	return imageReportJSON{
		DryRun:       dryRun,
		Images:       report.MSBImagesPruned,
		DockerImages: report.DockerImagesPruned,
		Details:      newPruneDetailsJSON(report.Details),
	}
}

func newVolumeReportJSON(report pruning.VolumeReport, dryRun bool) volumeReportJSON {
	return volumeReportJSON{
		DryRun:  dryRun,
		Volumes: report.VolumesPruned,
		Details: newPruneDetailsJSON(report.Details),
	}
}

func newSandboxReportJSON(report pruning.SandboxReport, dryRun bool) sandboxReportJSON {
	return sandboxReportJSON{
		DryRun:    dryRun,
		Sandboxes: report.VMsPruned,
		Details:   newPruneDetailsJSON(report.Details),
	}
}

func newPruneDetailsJSON(entries []pruning.StaleEntry) []pruneDetailJSON {
	details := make([]pruneDetailJSON, 0, len(entries))
	for _, entry := range entries {
		details = append(details, pruneDetailJSON{
			Type:            entry.Type.String(),
			Name:            entry.Name,
			Slug:            entry.Slug,
			Digest:          entry.Digest,
			StaleForSeconds: int64(entry.StaleFor.Seconds()),
		})
	}
	return details
}

// renderPruneReport writes the JSON value under --format json, or calls the
// human renderer by default. Quiet suppresses stdout entirely (prune has no
// identifiers-only mode). The values passed by callers are concrete report
// DTOs, so the JSON marshal cannot fail.
func renderPruneReport(ui termio.UI, mode listOutput, human func(), jsonValue any) {
	if mode == listOutputJSON {
		data, _ := json.MarshalIndent(jsonValue, "", "  ")
		ui.Out(string(data))
		return
	}
	if mode == listOutputTable {
		human()
	}
}
