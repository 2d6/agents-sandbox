# Runtime-management Slice 2 — private-home accounting seam

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a no-command accounting seam that reports the private runtime home's storage categories (byte sizes, host paths) for the later `df`/`storage` report.

**Architecture:** A new `internal/sandbox/storage` package composes two sources: microsandbox's `StorageUsage` for the six categories it owns, and a recursive host walk for the runtime/FFI artifacts under the private home (`bin`/`lib`). Nil msb counters stay nil (unknown, never zero); `ErrUnsupportedOperation` degrades to unknown categories rather than failing.

**Tech Stack:** Go 1.26, `github.com/superradcompany/microsandbox/sdk/go` v0.7.8 (`StorageUsage`), existing `internal/sandbox/msb` and `internal/sandbox/runtime` packages, testify.

**Spec:** `.operator-shared/specs/runtime-management-commands.md` (Slice 2, issue #170)

## Global Constraints

- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- No user-facing command in this slice; `df`/`storage` are Slice 5 (issue #155).
- Scope decided with the user: **six msb categories + runtime/FFI artifacts only**. Docker images and the BuildKit cache are deferred (host Docker's store is outside the private home; the BuildKit cache is a named msb volume already covered by the `volumes` category).
- Private runtime home only: derive paths from `runtime.PrivateHome()`; never read `~/.microsandbox`.
- Follow repo conventions: package-level `Get`/var seams for testability; `golangci-lint` is the linter (`make lint`), not `go vet`; format with `make fmt`.

## Review Focus

- **Nil vs zero:** a nil `*uint64` from microsandbox must render as unknown; a host directory that exists and is empty is a known zero. Tests pin both.
- **Unsupported backend:** `ErrUnsupportedOperation` must not drop the runtime category or return an error.
- **Corrupt/unreadable host subtree:** a walk error must propagate, not silently report zero.
- **Unknown msb error:** any other observation error propagates (only `ErrUnsupportedOperation` degrades).
- **Ordering:** category order is deterministic (msb six, then runtime) so Slice 5 output and JSON are stable.

---

### Task 1: Extend the msb seam with `StorageUsage`

**Files:**
- Modify: `internal/sandbox/msb/msb.go`
- Modify: `internal/sandbox/msb/testmock.go`
- Test: `internal/sandbox/msb/msb_test.go` (add cases) and `internal/sandbox/msb/testmock_test.go`

**Interfaces:**
- Consumes: `msbSdk.StorageUsage(ctx) (*msbSdk.StorageUsageReport, error)`, `msbSdk.IsKind(err, kind)`, `msbSdk.ErrUnsupportedOperation`.
- Produces:
  - `Client.StorageUsage(ctx context.Context) (*msbSdk.StorageUsageReport, error)`
  - `msb.IsUnsupportedOperation(err error) bool`
  - `MockMsbClient.StorageUsageFn func(context.Context) (*msbSdk.StorageUsageReport, error)` and `MockMsbClient.SetStorageUsageErr(err) *MockMsbClient`

- [ ] **Step 1: Write the failing tests**

In `internal/sandbox/msb/msb_test.go`:

```go
func TestIsUnsupportedOperation(t *testing.T) {
	err := &msbSdk.Error{Kind: msbSdk.ErrUnsupportedOperation, Message: "nope"}
	require.True(t, IsUnsupportedOperation(err))
	require.True(t, IsUnsupportedOperation(fmt.Errorf("wrap: %w", err)))
	require.False(t, IsUnsupportedOperation(&msbSdk.Error{Kind: msbSdk.ErrSandboxNotFound}))
	require.False(t, IsUnsupportedOperation(nil))
}
```

In `internal/sandbox/msb/testmock_test.go`:

```go
func TestMockStorageUsageReturnsReport(t *testing.T) {
	want := &msbSdk.StorageUsageReport{}
	m := &MockMsbClient{}
	m.StorageUsageFn = func(context.Context) (*msbSdk.StorageUsageReport, error) { return want, nil }
	got, err := m.StorageUsage(context.Background())
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestMockStorageUsageDefaultsAndError(t *testing.T) {
	m := &MockMsbClient{}
	got, err := m.StorageUsage(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)

	m = (&MockMsbClient{}).SetStorageUsageErr(errors.New("boom"))
	_, err = m.StorageUsage(context.Background())
	require.ErrorContains(t, err, "boom")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/msb/ -run 'Unsupported|MockStorageUsage' -v`
Expected: FAIL — `IsUnsupportedOperation`/`StorageUsage` undefined.

- [ ] **Step 3: Implement the seam**

Add to the `Client` interface in `msb.go` (after `ImageInspect`):

```go
StorageUsage(ctx context.Context) (*msbSdk.StorageUsageReport, error)
```

Add the real delegate:

```go
func (realMsbClient) StorageUsage(ctx context.Context) (*msbSdk.StorageUsageReport, error) {
	return msbSdk.StorageUsage(ctx)
}
```

Add the classifier next to `IsReplaced`:

```go
// IsUnsupportedOperation reports whether err indicates the backend or native
// library cannot observe storage (cloud or older native libraries), unwrapping
// any wrapped errors. Callers degrade the affected categories to unknown.
func IsUnsupportedOperation(err error) bool {
	return msbSdk.IsKind(err, msbSdk.ErrUnsupportedOperation)
}
```

In `testmock.go`: add the callback field, an error field, the method, the setter, and the fail-fast stub.

```go
StorageUsageFn func(ctx context.Context) (*msbSdk.StorageUsageReport, error)
// ...
storageUsageErr error
```

```go
// StorageUsage implements Client.
func (m *MockMsbClient) StorageUsage(ctx context.Context) (*msbSdk.StorageUsageReport, error) {
	if m.StorageUsageFn != nil {
		return m.StorageUsageFn(ctx)
	}
	if m.storageUsageErr != nil {
		return nil, m.storageUsageErr
	}
	return &msbSdk.StorageUsageReport{}, nil
}

// SetStorageUsageErr sets the error returned by MockMsbClient.StorageUsage.
func (m *MockMsbClient) SetStorageUsageErr(err error) *MockMsbClient {
	m.storageUsageErr = err
	return m
}
```

```go
func (f *failFastMsbClient) StorageUsage(_ context.Context) (*msbSdk.StorageUsageReport, error) {
	f.mustMock()
	return nil, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/msb/ -v`
Expected: PASS (all existing msb tests still pass).

- [ ] **Step 5: Lint and commit**

```bash
make fmt && make lint
git add internal/sandbox/msb/msb.go internal/sandbox/msb/testmock.go internal/sandbox/msb/msb_test.go internal/sandbox/msb/testmock_test.go
git commit -m "feat(msb): expose StorageUsage and unsupported-operation classifier"
```

---

### Task 2: `storage` package — types and msb categories (totals, nil = unknown)

**Files:**
- Create: `internal/sandbox/storage/storage.go`
- Create: `internal/sandbox/storage/main_test.go`
- Test: `internal/sandbox/storage/storage_test.go`

**Interfaces:**
- Consumes: `msb.Get() msb.Client`, `msb.Client.StorageUsage`, `msb.IsUnsupportedOperation`, `runtime.PrivateHome() string`.
- Produces:
  - `const ( CategoryImages = "images"; CategorySnapshots = "snapshots"; CategorySandboxes = "sandboxes"; CategoryVolumes = "volumes"; CategoryBranchMemory = "branch-memory"; CategorySnapshotMemory = "snapshot-memory"; CategoryRuntime = "runtime" )`
  - `type Options struct { IncludeObjects bool }`
  - `type Report struct { Categories []Category }`
  - `type Category struct { Name string; Totals Totals; Objects []Object; Notes []string }`
  - `type Totals struct { Count, LogicalBytes, AllocatedBytes, ReclaimableBytes *uint64 }`
  - `type Object struct { Name, Path string; LogicalBytes, AllocatedBytes *uint64; InUse, Reclaimable *bool; Reasons []string }`
  - `func Observe(ctx context.Context, opts Options) (*Report, error)`

- [ ] **Step 1: Write the failing test**

`internal/sandbox/storage/main_test.go` installs the fail-fast msb client for the package (mirror the pattern used by other sandbox packages; if the repo already has a shared helper, use it):

```go
package storage

import (
	"os"
	"testing"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
)

func TestMain(m *testing.M) {
	msb.InstallFailFastGet()
	os.Exit(m.Run())
}
```

`internal/sandbox/storage/storage_test.go`:

```go
package storage

import (
	"context"
	"testing"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
)

func uint64Ptr(v uint64) *uint64 { return &v }

func reportWithAllCategories() *msbSdk.StorageUsageReport {
	return &msbSdk.StorageUsageReport{
		Images:         msbSdk.StorageCategoryUsage{Count: uint64Ptr(2), LogicalBytes: uint64Ptr(100), AllocatedBytes: uint64Ptr(80)},
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
	t.Setenv("HOME", t.TempDir())
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	require.NoError(t, err)
	require.Equal(t, []string{
		CategoryImages, CategorySnapshots, CategorySandboxes, CategoryVolumes,
		CategoryBranchMemory, CategorySnapshotMemory, CategoryRuntime,
	}, categoryNames(report))

	images := report.Categories[0]
	require.Equal(t, uint64Ptr(2), images.Totals.Count)
	require.Equal(t, uint64Ptr(100), images.Totals.LogicalBytes)
	require.Equal(t, uint64Ptr(80), images.Totals.AllocatedBytes)
	require.Nil(t, images.Totals.ReclaimableBytes)
}

func TestObserveKeepsNilCountersUnknown(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{}, nil
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	require.NoError(t, err)
	for _, c := range report.Categories[:6] {
		require.Nil(t, c.Totals.Count, c.Name)
		require.Nil(t, c.Totals.LogicalBytes, c.Name)
		require.Nil(t, c.Totals.AllocatedBytes, c.Name)
		require.Nil(t, c.Totals.ReclaimableBytes, c.Name)
	}
}

func categoryNames(r *Report) []string {
	names := make([]string, len(r.Categories))
	for i, c := range r.Categories {
		names[i] = c.Name
	}
	return names
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/storage/ -v`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Implement types + msb mapping**

Create `storage.go` with the exported types/constants above, the seams, and:

```go
var (
	storageUsageOf = func(ctx context.Context) (*msbSdk.StorageUsageReport, error) {
		return msb.Get().StorageUsage(ctx)
	}
	privateHome = runtime.PrivateHome
)

func Observe(ctx context.Context, opts Options) (*Report, error) {
	categories := msbCategories()
	report, err := storageUsageOf(ctx)
	switch {
	case msb.IsUnsupportedOperation(err):
		markAllUnknown(categories, unsupportedStorageNote)
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
```

Implement `msbCategories`, `applyMSBUsage`, `totalsFrom`, `markAllUnknown`. `observeRuntime` is a stub returning an empty `Category{Name: CategoryRuntime}` for now (Task 4 fills it) so this task's tests pass.

Note: reference the `runtime` package by aliasing the import (e.g. `sandboxruntime "github.com/inoio/agents-sandbox/internal/sandbox/runtime"`) to avoid shadowing the stdlib `runtime` name.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt && make lint
git add internal/sandbox/storage/
git commit -m "feat(storage): observe msb storage categories with unknown-aware totals"
```

---

### Task 3: Degrade `ErrUnsupportedOperation` gracefully

**Files:**
- Modify: `internal/sandbox/storage/storage.go`
- Test: `internal/sandbox/storage/storage_test.go`

**Interfaces:**
- Consumes: Task 1 `msb.IsUnsupportedOperation`, Task 2 types.
- Produces: `Observe` returns all seven categories with nil msb totals + a note and no error when unsupported; any other observation error propagates.

- [ ] **Step 1: Write the failing tests**

```go
func TestObserveUnsupportedDegradesToUnknown(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return nil, &msbSdk.Error{Kind: msbSdk.ErrUnsupportedOperation, Message: "remote backend"}
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	require.NoError(t, err)
	require.Len(t, report.Categories, 7)
	for _, c := range report.Categories[:6] {
		require.Nil(t, c.Totals.Count, c.Name)
		require.NotEmpty(t, c.Notes, c.Name)
	}
	require.Equal(t, CategoryRuntime, report.Categories[6].Name)
}

func TestObserveMSBErrorPropagates(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return nil, errors.New("disk exploded")
	}})
	privateHome = func() string { return t.TempDir() }

	_, err := Observe(context.Background(), Options{})
	require.ErrorContains(t, err, "disk exploded")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/storage/ -run 'Unsupported|Propagates' -v`
Expected: FAIL if `markAllUnknown`/note not wired.

- [ ] **Step 3: Implement**

Wire `markAllUnknown` to append `unsupportedStorageNote` to each msb category's `Notes` (totals already nil). Define `const unsupportedStorageNote = "storage observation unsupported by this backend"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt && make lint
git add internal/sandbox/storage/
git commit -m "feat(storage): degrade unsupported storage observation to unknown"
```

---

### Task 4: Runtime/FFI host category (walk `bin`/`lib`)

**Files:**
- Create: `internal/sandbox/storage/host.go`
- Modify: `internal/sandbox/storage/storage.go` (replace the stub `observeRuntime`)
- Test: `internal/sandbox/storage/host_test.go`

**Interfaces:**
- Consumes: Task 2 `Category`, `Totals`, `Object`, `Options`.
- Produces: `observeRuntime(home string, opts Options) (Category, error)` — known `Count`/`LogicalBytes` from regular files under `<home>/bin` and `<home>/lib`; `AllocatedBytes`/`ReclaimableBytes` nil; a walk error propagates.

- [ ] **Step 1: Write the failing tests**

```go
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o644))
}

func TestObserveRuntimeSumsBinAndLib(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "bin", "msb"), 100)
	writeFile(t, filepath.Join(home, "lib", "libkrunfw.so.5.6.1"), 200)

	cat, err := observeRuntime(home, Options{})
	require.NoError(t, err)
	require.Equal(t, CategoryRuntime, cat.Name)
	require.Equal(t, uint64Ptr(2), cat.Totals.Count)
	require.Equal(t, uint64Ptr(300), cat.Totals.LogicalBytes)
	require.Nil(t, cat.Totals.AllocatedBytes)
	require.Nil(t, cat.Totals.ReclaimableBytes)
	require.Empty(t, cat.Objects) // not requested
}

func TestObserveRuntimeMissingHomeIsKnownZero(t *testing.T) {
	cat, err := observeRuntime(t.TempDir(), Options{})
	require.NoError(t, err)
	require.Equal(t, uint64Ptr(0), cat.Totals.Count)
	require.Equal(t, uint64Ptr(0), cat.Totals.LogicalBytes)
}

func TestObserveRuntimeWalkErrorPropagates(t *testing.T) {
	home := t.TempDir()
	// A symlink loop under bin makes the recursive walk fail.
	require.NoError(t, os.MkdirAll(filepath.Join(home, "bin"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(home, "bin"), filepath.Join(home, "bin", "loop")))
	_, err := observeRuntime(home, Options{})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/storage/ -run Runtime -v`
Expected: FAIL — stub returns empty totals.

- [ ] **Step 3: Implement `host.go`**

```go
func observeRuntime(home string, opts Options) (Category, error) {
	cat := Category{Name: CategoryRuntime}
	seen := map[string]bool{}
	var count, logical uint64
	var objects []Object
	for _, sub := range []string{"bin", "lib"} {
		root := filepath.Join(home, sub)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return fs.SkipDir
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			size := uint64(info.Size())
			count++
			logical += size
			if opts.IncludeObjects {
				s := size
				objects = append(objects, Object{Name: filepath.Base(path), Path: path, LogicalBytes: &s})
			}
			return nil
		})
		if err != nil {
			return Category{}, fmt.Errorf("scan runtime home %s: %w", root, err)
		}
	}
	_ = seen
	cat.Totals = Totals{Count: &count, LogicalBytes: &logical}
	cat.Objects = objects
	return cat, nil
}
```

Remove the unused `seen` map (it was only a sketch). Ensure `AllocatedBytes`/`ReclaimableBytes` remain nil.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/storage/ -v`
Expected: PASS. If the symlink-loop test cannot force an error on the platform (WalkDir may detect some loops as `ErrNotExist` on the symlink itself), replace it with an unreadable-directory case (`chmod 0o000` a `bin` subdir, skip when running as root).

- [ ] **Step 5: Commit**

```bash
make fmt && make lint
git add internal/sandbox/storage/
git commit -m "feat(storage): account runtime/FFI artifacts under the private home"
```

---

### Task 5: Per-object rows on request (`IncludeObjects`)

**Files:**
- Modify: `internal/sandbox/storage/storage.go` (`applyMSBUsage` object mapping)
- Test: `internal/sandbox/storage/storage_test.go`

**Interfaces:**
- Consumes: Task 2 types; msb `StorageItemUsage` (`Name`, `Path`, `LogicalBytes`, `AllocatedBytes`, `InUse`, `Reclaimable`, `Reasons`).
- Produces: `Options{IncludeObjects: true}` populates `Category.Objects` for msb categories and runtime; `false` leaves `Objects` nil.

- [ ] **Step 1: Write the failing tests**

```go
func TestObserveIncludeObjects(t *testing.T) {
	inUse := true
	reclaimable := false
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{
			Images: msbSdk.StorageCategoryUsage{
				Items: []msbSdk.StorageItemUsage{{
					Name: "agents-sandbox/x:latest", Path: "/home/.local/share/agents-sandbox/msb/images/x",
					LogicalBytes: uint64Ptr(500), AllocatedBytes: uint64Ptr(400),
					InUse: &inUse, Reclaimable: &reclaimable, Reasons: []string{"tagged"},
				}},
			},
		}, nil
	}})
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "bin", "msb"), 64)
	privateHome = func() string { return home }

	report, err := Observe(context.Background(), Options{IncludeObjects: true})
	require.NoError(t, err)
	require.Len(t, report.Categories[0].Objects, 1)
	obj := report.Categories[0].Objects[0]
	require.Equal(t, "agents-sandbox/x:latest", obj.Name)
	require.Equal(t, uint64Ptr(500), obj.LogicalBytes)
	require.Equal(t, &inUse, obj.InUse)
	require.Equal(t, []string{"tagged"}, obj.Reasons)
	require.Len(t, report.Categories[6].Objects, 1, "runtime object rows")
}

func TestObserveOmitsObjectsByDefault(t *testing.T) {
	msb.WithMsbMock(t, &msb.MockMsbClient{StorageUsageFn: func(context.Context) (*msbSdk.StorageUsageReport, error) {
		return &msbSdk.StorageUsageReport{Images: msbSdk.StorageCategoryUsage{
			Items: []msbSdk.StorageItemUsage{{Name: "x"}},
		}}, nil
	}})
	privateHome = func() string { return t.TempDir() }

	report, err := Observe(context.Background(), Options{})
	require.NoError(t, err)
	for _, c := range report.Categories {
		require.Nil(t, c.Objects, c.Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/storage/ -run IncludeObjects -v`
Expected: FAIL — objects not mapped.

- [ ] **Step 3: Implement object mapping**

Add `objectsFrom(items []msbSdk.StorageItemUsage) []Object` and call it from `applyMSBUsage` when `opts.IncludeObjects`. Ensure nil `Objects` when not requested (do not allocate an empty slice).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
make fmt && make lint
git add internal/sandbox/storage/
git commit -m "feat(storage): expose per-object rows on request"
```

---

### Task 6: Docs, changelog, and spec status

**Files:**
- Modify: `CHANGELOG.md` (`[Unreleased]`)
- Modify: `.operator-shared/specs/runtime-management-commands.md` (status line + Slice 2)

- [ ] **Step 1: Add the changelog line**

Under `[Unreleased]`, add an entry noting the new internal accounting seam (no user-facing command). Match the file's existing style.

- [ ] **Step 2: Update the spec status**

Update the status line to record Slice 2 as implemented (issue #170), alongside Slice 0's note.

- [ ] **Step 3: Run full verification**

Run: `make check`
Expected: fmt, lint, and tests pass. (Integration-only build failures unrelated to this change are acceptable per AGENTS.md; report them.)

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md .operator-shared/specs/runtime-management-commands.md
git commit -m "docs: record runtime-management Slice 2 accounting seam"
```

---

## Self-Review notes

- Spec coverage: Slice 2 asks for a seam enumerating private-home categories with byte sizes/host paths (Tasks 2–5), nil-as-unknown (Task 2), `ErrUnsupportedOperation` degradation (Task 3), and tests for known/nil/unsupported (Tasks 2–5). Docker/BuildKit deferred per the user's scope decision; documented in Global Constraints.
- Type consistency: `Totals` field `ReclaimableBytes` maps from msb's `ReclaimableLogicalBytes`; `Object` fields map 1:1 from `StorageItemUsage`.
- The `privateHome` var and `storageUsageOf` var are the test seams; tests override them rather than touching global config.
