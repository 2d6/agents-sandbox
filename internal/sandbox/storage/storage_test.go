package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
)

func uint64Ptr(v uint64) *uint64 { return new(v) }

func reportWithAllCategories() *msbSdk.StorageUsageReport {
	return &msbSdk.StorageUsageReport{
		Images: msbSdk.StorageCategoryUsage{
			Count:          uint64Ptr(2),
			LogicalBytes:   uint64Ptr(100),
			AllocatedBytes: uint64Ptr(80),
		},
		Snapshots:      msbSdk.StorageCategoryUsage{Count: uint64Ptr(1), LogicalBytes: uint64Ptr(10)},
		Sandboxes:      msbSdk.StorageCategoryUsage{Count: uint64Ptr(3), LogicalBytes: uint64Ptr(30)},
		Volumes:        msbSdk.StorageCategoryUsage{Count: uint64Ptr(4), LogicalBytes: uint64Ptr(40)},
		BranchMemory:   msbSdk.StorageCategoryUsage{Count: uint64Ptr(5), LogicalBytes: uint64Ptr(50)},
		SnapshotMemory: msbSdk.StorageCategoryUsage{Count: uint64Ptr(6), LogicalBytes: uint64Ptr(60)},
	}
}

func TestObserveMapsMSBCategoriesInOrder(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return reportWithAllCategories(), nil
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got, want := categoryNames(report), []string{
		CategoryImages, CategorySnapshots, CategorySandboxes, CategoryVolumes,
		CategoryBranchMemory, CategorySnapshotMemory, CategoryRuntime,
	}; !equalStrings(got, want) {
		t.Fatalf("category names = %v, want %v", got, want)
	}

	images := report.Categories[0]
	if got := images.Totals.Count; got == nil || *got != 2 {
		t.Errorf("images count = %v, want 2", got)
	}
	if got := images.Totals.LogicalBytes; got == nil || *got != 100 {
		t.Errorf("images logical bytes = %v, want 100", got)
	}
	if got := images.Totals.AllocatedBytes; got == nil || *got != 80 {
		t.Errorf("images allocated bytes = %v, want 80", got)
	}
	if images.Totals.ReclaimableBytes != nil {
		t.Errorf("images reclaimable bytes = %v, want nil", images.Totals.ReclaimableBytes)
	}
}

func TestObserveKeepsNilCountersUnknown(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{}, nil
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	for _, c := range report.Categories[:6] {
		if c.Totals.Count != nil || c.Totals.LogicalBytes != nil ||
			c.Totals.AllocatedBytes != nil || c.Totals.ReclaimableBytes != nil {
			t.Errorf("category %s totals = %+v, want all nil (unknown)", c.Name, c.Totals)
		}
	}
}

func TestObserveUnsupportedDegradesToUnknown(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return nil, &msbSdk.Error{Kind: msbSdk.ErrUnsupportedOperation, Message: "remote backend"}
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Observe() error = %v, want nil (graceful degrade)", err)
	}
	if len(report.Categories) != 7 {
		t.Fatalf("categories = %d, want 7", len(report.Categories))
	}
	for _, c := range report.Categories[:6] {
		if c.Totals.Count != nil {
			t.Errorf("category %s count = %v, want nil (unknown)", c.Name, c.Totals.Count)
		}
		if len(c.Notes) == 0 {
			t.Errorf("category %s notes = empty, want unsupported note", c.Name)
		}
	}
	if report.Categories[6].Name != CategoryRuntime {
		t.Errorf("last category = %s, want runtime", report.Categories[6].Name)
	}
}

func TestObserveMSBErrorPropagates(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return nil, errors.New("disk exploded")
	}})
	privateHome = func() string { return t.TempDir() }

	if _, err := Observe(context.Background(), Options{}); err == nil {
		t.Fatal("Observe() error = nil, want propagation of non-unsupported error")
	}
}

func TestObserveIncludeObjects(t *testing.T) {
	inUse := true
	reclaimable := false
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{
			Images: msbSdk.StorageCategoryUsage{
				Items: []msbSdk.StorageItemUsage{{
					Name:           "agents-sandbox/x:latest",
					Path:           "/home/.local/share/agents-sandbox/msb/images/x",
					LogicalBytes:   uint64Ptr(500),
					AllocatedBytes: uint64Ptr(400),
					InUse:          &inUse,
					Reclaimable:    &reclaimable,
					Reasons:        []string{"tagged"},
				}},
			},
		}, nil
	}})
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "bin", "msb"), 64)
	privateHome = func() string { return home }

	report, err := Observe(context.Background(), Options{IncludeObjects: true})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if len(report.Categories[0].Objects) != 1 {
		t.Fatalf("images objects = %d, want 1", len(report.Categories[0].Objects))
	}
	object := report.Categories[0].Objects[0]
	if object.Name != "agents-sandbox/x:latest" {
		t.Errorf("object name = %q", object.Name)
	}
	if got := object.LogicalBytes; got == nil || *got != 500 {
		t.Errorf("object logical bytes = %v, want 500", got)
	}
	if object.InUse != &inUse || object.Reclaimable != &reclaimable {
		t.Errorf("object flags = %v/%v, want in-use/reclaimable", object.InUse, object.Reclaimable)
	}
	if len(object.Reasons) != 1 || object.Reasons[0] != "tagged" {
		t.Errorf("object reasons = %v", object.Reasons)
	}
	if got := report.Categories[6].Objects; len(got) != 1 {
		t.Errorf("runtime objects = %d, want 1", len(got))
	}
}

func TestObserveOmitsObjectsByDefault(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{Images: msbSdk.StorageCategoryUsage{
			Items: []msbSdk.StorageItemUsage{{Name: "x"}},
		}}, nil
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	for _, c := range report.Categories {
		if c.Objects != nil {
			t.Errorf("category %s objects = %v, want nil", c.Name, c.Objects)
		}
	}
}

func categoryNames(r *Report) []string {
	names := make([]string, len(r.Categories))
	for i, c := range r.Categories {
		names[i] = c.Name
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
