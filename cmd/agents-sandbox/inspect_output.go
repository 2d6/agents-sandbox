package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/inoio/agents-sandbox/internal/humanize"
	"github.com/inoio/agents-sandbox/internal/sandbox/image"
	sandbox "github.com/inoio/agents-sandbox/internal/sandbox/vm"
	"github.com/inoio/agents-sandbox/internal/sandbox/volume"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// Key/value row labels shared by more than one inspect renderer.
const (
	inspectKeyLabels  = "labels"
	inspectKeyCreated = "created"
)

// printInspect renders a single-object inspection result: the identifier only
// under quiet, a structured value under --format json, or the default human
// key/value block. Callers pass the identifier printed by quiet mode, the
// human renderer, and the JSON DTO.
func printInspect(ui termio.UI, mode listOutput, identifier string, human func(), jsonValue any) error {
	switch mode {
	case listOutputIdentifiers:
		ui.Print(identifier)
	case listOutputJSON:
		data, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			return err
		}
		ui.Out(string(data))
	case listOutputTable:
		human()
	}
	return nil
}

// printKeyValues renders an aligned "key:  value" block, padding keys to the
// widest key so the values line up.
func printKeyValues(ui termio.UI, rows [][2]string) {
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		ui.Outf("%-*s  %s", width, row[0]+":", row[1])
	}
}

// formatBytesOrDash humanizes a byte count, or renders "-" for an absent one.
func formatBytesOrDash(bytes *uint64) string {
	if bytes == nil {
		return "-"
	}
	return humanize.FormatBytes(*bytes)
}

// formatMiBOrDash humanizes a MiB count as bytes, or renders "-" for an absent
// one.
func formatMiBOrDash(mib *uint32) string {
	if mib == nil {
		return "-"
	}
	return humanize.FormatBytes(uint64(*mib) * 1024 * 1024)
}

// derefStringOrDash returns the pointed-to string, or "-" for nil.
func derefStringOrDash(value *string) string {
	if value == nil {
		return "-"
	}
	return *value
}

// joinLabels renders a label map as a stable "k=v, k2=v2" string, or "-" when
// empty.
func joinLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+labels[key])
	}
	return strings.Join(pairs, ", ")
}

// joinOrDash joins a string slice with ", ", or renders "-" when empty.
func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

// nonNilStrings returns the slice unchanged, or an empty slice when nil, so a
// JSON DTO emits [] instead of null.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// -- volume --

func printVolumeInspect(ui termio.UI, detail volume.VolumeDetail) {
	printKeyValues(ui, [][2]string{
		{"name", detail.Name},
		{"kind", detail.Kind},
		{"path", detail.Path},
		{"quota", formatMiBOrDash(detail.QuotaMiB)},
		{"used", humanize.FormatBytes(detail.UsedBytes)},
		{"capacity", formatBytesOrDash(detail.CapacityBytes)},
		{"disk format", derefStringOrDash(detail.DiskFormat)},
		{"disk fstype", derefStringOrDash(detail.DiskFstype)},
		{inspectKeyLabels, joinLabels(detail.Labels)},
		{inspectKeyCreated, detail.CreatedAt},
	})
}

type jsonVolumeInspect struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Path       string            `json:"path"`
	Quota      string            `json:"quota"`
	Used       string            `json:"used"`
	Capacity   string            `json:"capacity"`
	DiskFormat string            `json:"disk_format"`
	DiskFstype string            `json:"disk_fstype"`
	Labels     map[string]string `json:"labels"`
	Created    string            `json:"created"`
}

func newVolumeInspectJSON(detail volume.VolumeDetail) jsonVolumeInspect {
	return jsonVolumeInspect{
		Name:       detail.Name,
		Kind:       detail.Kind,
		Path:       detail.Path,
		Quota:      formatMiBOrDash(detail.QuotaMiB),
		Used:       humanize.FormatBytes(detail.UsedBytes),
		Capacity:   formatBytesOrDash(detail.CapacityBytes),
		DiskFormat: derefStringOrDash(detail.DiskFormat),
		DiskFstype: derefStringOrDash(detail.DiskFstype),
		Labels:     detail.Labels,
		Created:    detail.CreatedAt,
	}
}

// -- image --

func printImageInspect(ui termio.UI, detail image.ImageDetail) {
	printKeyValues(ui, [][2]string{
		{"reference", detail.Reference},
		{"digest", detail.Digest},
		{"size", detail.Size},
		{inspectKeyCreated, detail.CreatedAt},
		{"working dir", detail.WorkingDir},
		{"user", detail.User},
		{"stop signal", detail.StopSignal},
		{"env", joinOrDash(detail.Env)},
		{"cmd", joinOrDash(detail.Cmd)},
		{"entrypoint", joinOrDash(detail.Entrypoint)},
		{inspectKeyLabels, joinLabels(detail.Labels)},
	})
	if len(detail.Layers) > 0 {
		ui.Outf("layers (%d):", len(detail.Layers))
		for _, layer := range detail.Layers {
			ui.Outf("  %d  %s  %s  erofs %s  %s",
				layer.Position, layer.DiffID, layer.CompressedSize, layer.ErofsSize, layer.MediaType)
		}
	}
}

type jsonImageLayer struct {
	Position       int32  `json:"position"`
	DiffID         string `json:"diff_id"`
	BlobDigest     string `json:"blob_digest"`
	MediaType      string `json:"media_type"`
	CompressedSize string `json:"compressed_size"`
	ErofsSize      string `json:"erofs_size"`
}

type jsonImageInspect struct {
	Reference  string            `json:"reference"`
	Digest     string            `json:"digest"`
	Size       string            `json:"size"`
	Created    string            `json:"created"`
	Env        []string          `json:"env"`
	Cmd        []string          `json:"cmd"`
	Entrypoint []string          `json:"entrypoint"`
	WorkingDir string            `json:"working_dir"`
	User       string            `json:"user"`
	Labels     map[string]string `json:"labels"`
	StopSignal string            `json:"stop_signal"`
	Layers     []jsonImageLayer  `json:"layers"`
}

func newImageInspectJSON(detail image.ImageDetail) jsonImageInspect {
	layers := make([]jsonImageLayer, 0, len(detail.Layers))
	for _, layer := range detail.Layers {
		layers = append(layers, jsonImageLayer{
			Position:       layer.Position,
			DiffID:         layer.DiffID,
			BlobDigest:     layer.BlobDigest,
			MediaType:      layer.MediaType,
			CompressedSize: layer.CompressedSize,
			ErofsSize:      layer.ErofsSize,
		})
	}
	return jsonImageInspect{
		Reference:  detail.Reference,
		Digest:     detail.Digest,
		Size:       detail.Size,
		Created:    detail.CreatedAt,
		Env:        nonNilStrings(detail.Env),
		Cmd:        nonNilStrings(detail.Cmd),
		Entrypoint: nonNilStrings(detail.Entrypoint),
		WorkingDir: detail.WorkingDir,
		User:       detail.User,
		Labels:     detail.Labels,
		StopSignal: detail.StopSignal,
		Layers:     layers,
	}
}

// -- sandbox --

func printSandboxInspect(ui termio.UI, detail sandbox.SandboxDetail) {
	printKeyValues(ui, [][2]string{
		{"name", detail.Name},
		{"id", detail.ID},
		{"status", detail.Status},
		{"image", detail.Image},
		{"backend", detail.Backend},
		{inspectKeyCreated, detail.CreatedAt},
		{"updated", detail.UpdatedAt},
		{"memory", formatMiBOrDash(&detail.MemoryMiB)},
		{"cpus", strconv.FormatUint(uint64(detail.CPUs), 10)},
		{"max memory", formatMiBOrDash(&detail.MaxMemoryMiB)},
		{"max cpus", strconv.FormatUint(uint64(detail.MaxCPUs), 10)},
		{inspectKeyLabels, joinLabels(detail.Labels)},
	})
	if len(detail.Mounts) > 0 {
		ui.Outf("mounts (%d):", len(detail.Mounts))
		for _, mount := range detail.Mounts {
			readonly := ""
			if mount.Readonly {
				readonly = "  (read-only)"
			}
			ui.Outf("  %s  %s  %s%s", mount.GuestPath, mount.Kind, mount.Source, readonly)
		}
	}
}

type jsonSandboxMount struct {
	GuestPath string `json:"guest_path"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Readonly  bool   `json:"readonly"`
}

type jsonSandboxInspect struct {
	Name         string             `json:"name"`
	ID           string             `json:"id"`
	Status       string             `json:"status"`
	Image        string             `json:"image"`
	Backend      string             `json:"backend"`
	Created      string             `json:"created"`
	Updated      string             `json:"updated"`
	Labels       map[string]string  `json:"labels"`
	Mounts       []jsonSandboxMount `json:"mounts"`
	MemoryMiB    uint32             `json:"memory_mib"`
	CPUs         uint8              `json:"cpus"`
	MaxMemoryMiB uint32             `json:"max_memory_mib"`
	MaxCPUs      uint8              `json:"max_cpus"`
}

func newSandboxInspectJSON(detail sandbox.SandboxDetail) jsonSandboxInspect {
	mounts := make([]jsonSandboxMount, 0, len(detail.Mounts))
	for _, mount := range detail.Mounts {
		mounts = append(mounts, jsonSandboxMount{
			GuestPath: mount.GuestPath,
			Kind:      mount.Kind,
			Source:    mount.Source,
			Readonly:  mount.Readonly,
		})
	}
	return jsonSandboxInspect{
		Name:         detail.Name,
		ID:           detail.ID,
		Status:       detail.Status,
		Image:        detail.Image,
		Backend:      detail.Backend,
		Created:      detail.CreatedAt,
		Updated:      detail.UpdatedAt,
		Labels:       detail.Labels,
		Mounts:       mounts,
		MemoryMiB:    detail.MemoryMiB,
		CPUs:         detail.CPUs,
		MaxMemoryMiB: detail.MaxMemoryMiB,
		MaxCPUs:      detail.MaxCPUs,
	}
}
