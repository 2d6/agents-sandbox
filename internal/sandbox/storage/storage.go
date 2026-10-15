// Package storage accounts for the private runtime home's storage: the
// categories microsandbox reports through its storage observation, plus the
// runtime/FFI artifacts agents-sandbox installs itself. It is groundwork for the
// later `df`/`storage` report and has no user-facing command.
package storage

import (
	"context"
	"fmt"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	sandboxruntime "github.com/inoio/agents-sandbox/internal/sandbox/runtime"
)

// Category names. The first six are owned and reported by microsandbox; the
// runtime category is the host-side runtime/FFI install.
const (
	CategoryImages         = "images"
	CategorySnapshots      = "snapshots"
	CategorySandboxes      = "sandboxes"
	CategoryVolumes        = "volumes"
	CategoryBranchMemory   = "branch-memory"
	CategorySnapshotMemory = "snapshot-memory"
	CategoryRuntime        = "runtime"
)

const unsupportedStorageNote = "storage observation unsupported by this backend"

// Options controls what Observe reports.
type Options struct {
	// IncludeObjects adds per-object rows to each category.
	IncludeObjects bool
}

// Report is the private runtime home's storage usage, split into categories.
type Report struct {
	Categories []Category
}

// Category is one storage category's totals and, when requested, its objects.
//
//exhaustruct:ignore
type Category struct {
	Name    string
	Totals  Totals
	Objects []Object
	Notes   []string
}

// Totals are a category's byte counts and object count. A nil pointer means
// unknown, never zero.
//
//exhaustruct:ignore
type Totals struct {
	Count            *uint64
	LogicalBytes     *uint64
	AllocatedBytes   *uint64
	ReclaimableBytes *uint64
}

// Object is one storage object: a microsandbox item or a host runtime artifact.
//
//exhaustruct:ignore
type Object struct {
	Name           string
	Path           string
	LogicalBytes   *uint64
	AllocatedBytes *uint64
	InUse          *bool
	Reclaimable    *bool
	Reasons        []string
}

// storageUsageOf and privateHome are seams for tests.
var (
	storageUsageOf = func(ctx context.Context) (*msbSdk.StorageUsageReport, error) {
		return msb.Get().StorageUsage(ctx)
	}
	privateHome = sandboxruntime.PrivateHome
)

// Observe enumerates the private runtime home's storage categories. The
// microsandbox observation supplies the categories it owns; the runtime/FFI
// artifacts are walked from the private home. An unsupported observation marks
// its categories unknown rather than failing; any other error propagates.
func Observe(ctx context.Context, opts Options) (*Report, error) {
	categories := msbCategories()
	report, err := storageUsageOf(ctx)
	switch {
	case msb.IsUnsupportedOperation(err):
		markAllUnknown(categories)
	case err != nil:
		return nil, fmt.Errorf("observe microsandbox storage: %w", err)
	case report != nil:
		applyMSBUsage(categories, report, opts)
	}

	runtimeCategory, err := observeRuntime(privateHome(), opts)
	if err != nil {
		return nil, err
	}
	categories = append(categories, runtimeCategory)
	return &Report{Categories: categories}, nil
}

// msbCategorySources pairs each microsandbox-owned category name with the
// selector for its usage in a report, so the report order and the usage mapping
// share one source of truth.
var msbCategorySources = []msbCategorySource{
	{name: CategoryImages, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.Images
	}},
	{name: CategorySnapshots, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.Snapshots
	}},
	{name: CategorySandboxes, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.Sandboxes
	}},
	{name: CategoryVolumes, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.Volumes
	}},
	{name: CategoryBranchMemory, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.BranchMemory
	}},
	{name: CategorySnapshotMemory, usageOf: func(r *msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage {
		return r.SnapshotMemory
	}},
}

// msbCategorySource names a microsandbox category and selects its usage.
//
//exhaustruct:ignore
type msbCategorySource struct {
	name    string
	usageOf func(*msbSdk.StorageUsageReport) msbSdk.StorageCategoryUsage
}

// msbCategories returns the six microsandbox-owned categories in report order,
// with unknown (nil) totals until usage is applied.
func msbCategories() []Category {
	categories := make([]Category, len(msbCategorySources))
	for i, source := range msbCategorySources {
		categories[i] = Category{Name: source.name}
	}
	return categories
}

// markAllUnknown notes on each category that the backend cannot observe storage,
// leaving the totals nil (unknown).
func markAllUnknown(categories []Category) {
	for i := range categories {
		categories[i].Notes = append(categories[i].Notes, unsupportedStorageNote)
	}
}

// applyMSBUsage fills each category from the microsandbox report, preserving nil
// counters as unknown and adding per-object rows when requested.
func applyMSBUsage(categories []Category, report *msbSdk.StorageUsageReport, opts Options) {
	for i, source := range msbCategorySources {
		usage := source.usageOf(report)
		categories[i].Totals = totalsFrom(usage)
		categories[i].Notes = append(categories[i].Notes, usage.Notes...)
		if opts.IncludeObjects {
			categories[i].Objects = objectsFrom(usage.Items)
		}
	}
}

// totalsFrom maps a microsandbox category usage, keeping optional counters nil
// so unknown stays unknown.
func totalsFrom(usage msbSdk.StorageCategoryUsage) Totals {
	return Totals{
		Count:            usage.Count,
		LogicalBytes:     usage.LogicalBytes,
		AllocatedBytes:   usage.AllocatedBytes,
		ReclaimableBytes: usage.ReclaimableLogicalBytes,
	}
}

// objectsFrom maps microsandbox items to objects, returning nil for none.
func objectsFrom(items []msbSdk.StorageItemUsage) []Object {
	if len(items) == 0 {
		return nil
	}
	objects := make([]Object, len(items))
	for i, item := range items {
		objects[i] = Object{
			Name:           item.Name,
			Path:           item.Path,
			LogicalBytes:   item.LogicalBytes,
			AllocatedBytes: item.AllocatedBytes,
			InUse:          item.InUse,
			Reclaimable:    item.Reclaimable,
			Reasons:        item.Reasons,
		}
	}
	return objects
}
