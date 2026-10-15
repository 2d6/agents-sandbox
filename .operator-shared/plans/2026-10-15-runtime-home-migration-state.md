# Legacy Home-Volume Migration State Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the legacy home-volume copy's unsafe completion model (an in-volume sentinel, no cross-process locking) with a host-owned `migrations` registry in root `state.yaml`, closing the three review findings on PR #142 (symlink-follow file truncation, concurrent copies deleting each other, guest-deletable completion losing newer data).

**Architecture:** Migration progress becomes the single source of truth for copy completion, stored host-side under a registry keyed by migration name. Legacy necessity and completion are derived from the filesystem plus this registry; the copy runs under a host-only advisory flock and records each finished volume in `copied`, each volume-scoped failure in `excluded`, collapsing to a terminal `completed`/`dismissed` word when the campaign ends. No file is ever written inside a guest-writable volume.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3`, `syscall.Flock` (via `internal/sandbox/state`), `testify`-style plain tests as used across `internal/`.

**Spec:** `.operator-shared/specs/runtime-home-decoupling.md` (see "Opt-in copy of legacy home volumes", "Host-side copy mechanics", "Persisted state").

## Global Constraints

- Target platforms: Linux (KVM) and macOS (Apple Silicon); no platform-specific code beyond what already exists.
- Never write inside a guest-writable volume; all migration state lives in root `state.yaml`.
- Persist via `internal/atomicwrite` (`state.WriteRootState` already does); no new state files.
- `dismissed`/`completed` are permanent with no re-arm.
- `--yes` and non-interactive sessions never copy and block with `ErrMigrationPending` while the migration is necessary and undismissed.
- The legacy source is read-only; a volume-scoped failure records the volume in `excluded` and continues; a campaign-scoped failure aborts and leaves `status: copying`.
- Lint/format via `golangci-lint` (`make lint`, `make fmt`); never `go vet`/`go fmt`. Run `make check` before finalizing.

## Review Focus

- **Concurrent interactive sessions both choosing "copy"** — the second must not discard or re-copy a volume the first is copying, and both must converge on `completed` (Task 3: flock test).
- **A crash/kill mid-copy** — the next session must treat present-but-unrecorded private volumes as partials, re-copy exactly those, and leave `copied` volumes alone (Task 3: resume test).
- **An unreadable/undeletable legacy volume** — must be auto-excluded, the remaining volumes still copied, and the campaign still reach `completed` (Task 3: exclusion test).
- **A legacy volume containing a symlink named `.agents-sandbox-copy-complete`** pointing at a host file — the host must not write through it (the old `os.WriteFile` truncated the target); the file must survive and no marker is created (Task 3: symlink test).
- **A terminal migration (`completed`/`dismissed`)** — must never re-offer or re-probe, even when a legacy volume is deleted from the marker's-eye view (Task 4: terminal test).

---

### Task 1: Migration registry schema in `state`

**Files:**
- Modify: `internal/sandbox/state/root.go`
- Modify: `internal/sandbox/runtimemigration/offer.go`
- Test: `internal/sandbox/state/root_test.go`, `internal/sandbox/runtimemigration/offer_test.go`

**Interfaces:**
- Consumes: `atomicwrite.WriteFile`, `yaml.Marshal` (existing `marshalRootState` seam), `stateRoot()`.
- Produces:
  - `type MigrationStatus string`; consts `MigrationCopying = "copying"`, `MigrationCompleted = "completed"`, `MigrationDismissed = "dismissed"`.
  - `type Migration struct { Status MigrationStatus; Copied []string; Excluded []string }` with yaml tags `status`, `copied,omitempty`, `excluded,omitempty`.
  - `RootState.Migrations map[string]Migration` with yaml tag `migrations,omitempty`.
  - `func (s *RootState) Migration(name string) (Migration, bool)` — missing registry or name returns `(Migration{}, false)` (the pending state).
  - `func (s *RootState) SetMigration(name string, m Migration)` — initializes the map if nil.
  - `func UpdateRootState(mutate func(*RootState)) error` — read-modify-write through `ReadRootState`/`WriteRootState`.
  - `const MigrationName = "runtime_home"` (package `runtimemigration`) — introduced here in `offer.go`, consumed by Tasks 2–4.

- [ ] **Step 1: Write the failing tests**

Replace the `RuntimeHomeMigration` wire-key test and round-trip test in `internal/sandbox/state/root_test.go` with registry tests. Delete `TestWriteRootStateUsesDocumentedWireKeys`'s current body and the `RuntimeHomeMigration`-based round-trip in `TestWriteAndReadRootState_RoundTrip`; add:

```go
func TestMigrationRegistryRoundTrip(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	if err := WriteRootState(RootState{Migrations: map[string]Migration{
		"runtime_home": {Status: MigrationCopying, Copied: []string{"a"}, Excluded: []string{"b"}},
	}}); err != nil {
		t.Fatalf("WriteRootState: %v", err)
	}
	got, err := ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	m, ok := got.Migration("runtime_home")
	if !ok || m.Status != MigrationCopying || len(m.Copied) != 1 || m.Copied[0] != "a" || len(m.Excluded) != 1 {
		t.Fatalf("migration = %+v, ok = %v", m, ok)
	}
}

func TestMigrationWireKeys(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	if err := WriteRootState(RootState{Migrations: map[string]Migration{
		"runtime_home": {Status: MigrationCompleted},
	}}); err != nil {
		t.Fatalf("WriteRootState: %v", err)
	}
	data, err := os.ReadFile(rootStateFile())
	if err != nil {
		t.Fatalf("read root state: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reg, ok := raw["migrations"].(map[string]any)
	if !ok {
		t.Fatalf("migrations = %v, want a mapping", raw["migrations"])
	}
	entry, ok := reg["runtime_home"].(map[string]any)
	if !ok || entry["status"] != "completed" {
		t.Fatalf("runtime_home entry = %v, want status completed", reg["runtime_home"])
	}
}

func TestMigrationAbsentIsPending(t *testing.T) {
	var s RootState
	if m, ok := s.Migration("runtime_home"); ok || m.Status != "" {
		t.Fatalf("Migration() = %+v, %v; want zero and false", m, ok)
	}
}

func TestSetMigrationInitializesRegistry(t *testing.T) {
	var s RootState
	s.SetMigration("runtime_home", Migration{Status: MigrationDismissed})
	if s.Migrations["runtime_home"].Status != MigrationDismissed {
		t.Fatalf("Migrations = %+v", s.Migrations)
	}
}

func TestUpdateRootStatePreservesOtherMigrations(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	if err := WriteRootState(RootState{Migrations: map[string]Migration{
		"other": {Status: MigrationCompleted},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := UpdateRootState(func(s *RootState) {
		s.SetMigration("runtime_home", Migration{Status: MigrationDismissed})
	}); err != nil {
		t.Fatalf("UpdateRootState: %v", err)
	}
	got, _ := ReadRootState()
	if got.Migrations["other"].Status != MigrationCompleted {
		t.Errorf("other migration lost: %+v", got.Migrations)
	}
	if got.Migrations["runtime_home"].Status != MigrationDismissed {
		t.Errorf("runtime_home not set: %+v", got.Migrations)
	}
}
```

Update the existing `TestWriteRootState_MarshalError`, `TestWriteRootState_MkdirAllError`, `TestWriteRootState_RenameErrorCleansTemp`, and `TestWriteRootState_ConcurrentWritesDoNotCollide` to construct `RootState{Migrations: map[string]Migration{"runtime_home": {Status: MigrationDismissed}}}` instead of `RuntimeHomeMigration: RuntimeHomeMigration{Dismissed: true}`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/state/ -run 'TestMigration|TestSetMigration|TestUpdateRootState' -v`
Expected: compile error — `Migration`, `MigrationStatus`, `RootState.Migrations`, `UpdateRootState` undefined.

- [ ] **Step 3: Implement the schema in `internal/sandbox/state/root.go`**

Replace the `RootState` struct body and the `RuntimeHomeMigration` type:

```go
// RootState is the launcher-global state document at the top of the state root,
// distinct from per-project state files under <root>/<slug>/<agent>/state.yaml.
//
//exhaustruct:ignore
type RootState struct {
	Migrations map[string]Migration `yaml:"migrations,omitempty"`
}

// MigrationStatus is the terminal or in-flight state of a one-time migration.
// The absent entry (no key) is the pending state and has no status value.
type MigrationStatus string

const (
	MigrationCopying   MigrationStatus = "copying"
	MigrationCompleted MigrationStatus = "completed"
	MigrationDismissed MigrationStatus = "dismissed"
)

// Migration tracks one one-time migration. Copied and Excluded are written only
// while Status is MigrationCopying and dropped when the migration reaches a
// terminal status.
//
//exhaustruct:ignore
type Migration struct {
	Status   MigrationStatus `yaml:"status"`
	Copied   []string        `yaml:"copied,omitempty"`
	Excluded []string        `yaml:"excluded,omitempty"`
}

// Migration returns the named migration and whether it is present. A missing
// registry or entry is the pending state: the zero Migration.
func (s *RootState) Migration(name string) (Migration, bool) {
	m, ok := s.Migrations[name]
	return m, ok
}

// SetMigration records a migration entry, initializing the registry if nil.
func (s *RootState) SetMigration(name string, m Migration) {
	if s.Migrations == nil {
		s.Migrations = map[string]Migration{}
	}
	s.Migrations[name] = m
}

// UpdateRootState reads the launcher-global state (a missing file yields a zero
// RootState), applies mutate, and writes it back atomically. It is a
// read-modify-write so a writer preserves entries it does not touch.
func UpdateRootState(mutate func(*RootState)) error {
	s, err := ReadRootState()
	if err != nil {
		return err
	}
	mutate(s)
	return WriteRootState(*s)
}
```

- [ ] **Step 4: Switch `offer.go` to the registry (keeps the module compiling)**

Removing `RuntimeHomeMigration` breaks `internal/sandbox/runtimemigration/offer.go`, so migrate its call sites in this task. Add `const MigrationName = "runtime_home"`. Replace the `rootState.RuntimeHomeMigration.Dismissed` read with a `root.Migration(MigrationName)` status check (`state.MigrationDismissed`), and replace the start-fresh `state.WriteRootState(state.RootState{RuntimeHomeMigration: state.RuntimeHomeMigration{Dismissed: true}})` with `state.UpdateRootState(func(root *state.RootState) { root.SetMigration(MigrationName, state.Migration{Status: state.MigrationDismissed}) })`. Leave the `NecessaryVolumes()` and `CopyVolumes(ctx, client, names, ui)` calls unchanged (later tasks change them). Update the `RuntimeHomeMigration` references in `offer_test.go` (`TestOfferDismissedDoesNotPrompt`, `TestOfferChooseStartFreshPersistsDismissal`, `TestOfferChooseQuitAborts`, `TestOfferSelectErrorAborts`) to read/write via `root.Migration(MigrationName)`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/sandbox/state/ ./internal/sandbox/runtimemigration/ -v`
Expected: PASS.

- [ ] **Step 6: Verify the whole module compiles**

Run: `go build ./...`
Expected: no errors (confirms no remaining caller of the removed `RuntimeHomeMigration`).

- [ ] **Step 7: Lint and commit**

```bash
make fmt && make lint
git add internal/sandbox/state/root.go internal/sandbox/state/root_test.go internal/sandbox/runtimemigration/offer.go internal/sandbox/runtimemigration/offer_test.go
git commit -m "feat(state): add a migrations registry to root state"
```

---

### Task 2: Status-aware necessity in `runtimemigration`

**Files:**
- Modify: `internal/sandbox/runtimemigration/legacy.go`
- Test: `internal/sandbox/runtimemigration/legacy_test.go`

**Interfaces:**
- Consumes: `state.Migration`, `state.MigrationStatus`, `state.MigrationCopying/Completed/Dismissed`, `state.ReadRootState`, `runtime.LegacyHome`, `runtime.PrivateHome`; `naming.HomePrefix`.
- Produces:
  - `const MigrationName = "runtime_home"` (package `runtimemigration`).
  - `func necessaryVolumes(legacyHome, privateHome string, m state.Migration) ([]string, error)` — replaces the two-arg form.
  - `func necessaryVolumesFor(m state.Migration) ([]string, error)` — resolves both homes (via `runtime.LegacyHome()`/`runtime.PrivateHome()`) and delegates.
  - `func presentPrivateVolumeNames(privateHome string) ([]string, error)` — renamed from `privateVolumeNames`, no marker check.
  - `Necessary()` / `NecessaryVolumes()` unchanged signatures; both now read root state to obtain the `MigrationName` entry.

Rules for `necessaryVolumes`:
- status `completed`/`dismissed` → `nil, nil`.
- status `copying` → legacy names minus `Copied` minus `Excluded` (the private listing is not consulted; an unrecorded present volume is a partial).
- otherwise (pending) → legacy names not present in the private home (`presentPrivateVolumeNames`).

- [ ] **Step 1: Write the failing tests**

In `internal/sandbox/runtimemigration/legacy_test.go`, replace `addCompletedVolumeDir` with a helper that does not write a marker, delete `TestNecessaryVolumesIncompletePrivateCopyIsNecessary` and `TestPrivateVolumeNamesFiltersByPrefix` (marker-based), and update every `necessaryVolumes(a, b)` call to `necessaryVolumes(a, b, state.Migration{})` (import `state`). Add:

```go
func TestNecessaryVolumesTerminalStatusIsNeverNecessary(t *testing.T) {
	for _, status := range []state.MigrationStatus{state.MigrationCompleted, state.MigrationDismissed} {
		legacy := newHome(t)
		addVolumeDir(t, legacy, naming.HomePrefix+"orphan")
		got, err := necessaryVolumes(legacy, newHome(t), state.Migration{Status: status})
		if err != nil || len(got) != 0 {
			t.Fatalf("status %q: got %v, %v; want empty", status, got, err)
		}
	}
}

func TestNecessaryVolumesCopyingSubtractsCopiedAndExcluded(t *testing.T) {
	legacy := newHome(t)
	private := newHome(t)
	copied := naming.HomePrefix + "copied"
	excluded := naming.HomePrefix + "excluded"
	pending := naming.HomePrefix + "pending"
	addVolumeDir(t, legacy, copied)
	addVolumeDir(t, legacy, excluded)
	addVolumeDir(t, legacy, pending)
	m := state.Migration{Status: state.MigrationCopying, Copied: []string{copied}, Excluded: []string{excluded}}
	got, err := necessaryVolumes(legacy, private, m)
	if err != nil {
		t.Fatalf("necessaryVolumes: %v", err)
	}
	assertSameSet(t, got, []string{pending})
}

func TestNecessaryVolumesCopyingIgnoresPrivatePresence(t *testing.T) {
	legacy := newHome(t)
	private := newHome(t)
	name := naming.HomePrefix + "partial"
	addVolumeDir(t, legacy, name)
	addVolumeDir(t, private, name) // present but NOT in Copied => partial, still necessary
	got, err := necessaryVolumes(legacy, private, state.Migration{Status: state.MigrationCopying})
	if err != nil {
		t.Fatalf("necessaryVolumes: %v", err)
	}
	assertSameSet(t, got, []string{name})
}
```

Update `TestNecessaryVolumesPresentInBothIsNotNecessary` and `TestNecessaryVolumesReportsOnlyMissingLegacyNames` to use `addVolumeDir` (not the completed variant) for the private side, since presence alone now means copied under pending.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/runtimemigration/ -run TestNecessary -v`
Expected: compile error — `necessaryVolumes` arity mismatch, `state` unused/missing.

- [ ] **Step 3: Implement in `internal/sandbox/runtimemigration/legacy.go`**

`MigrationName` already exists (defined in `offer.go` by Task 1). Change `necessaryVolumes` to the three-arg status-aware form above. Rename `privateVolumeNames` → `presentPrivateVolumeNames` and drop the marker filtering (return all `naming.HomePrefix`-prefixed dirs). Delete `hasCopyCompleteMarker` and the `strings`-using marker logic if now unused (keep `strings` for the prefix check). Rewrite `Necessary`/`NecessaryVolumes` to read root state:

```go
func NecessaryVolumes() ([]string, error) {
	root, err := state.ReadRootState()
	if err != nil {
		return nil, err
	}
	m, _ := root.Migration(MigrationName)
	return necessaryVolumesFor(m)
}

func necessaryVolumesFor(m state.Migration) ([]string, error) {
	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return nil, err
	}
	return necessaryVolumes(legacyHome, runtime.PrivateHome(), m)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/runtimemigration/ -run TestNecessary -v`
Expected: PASS.

- [ ] **Step 5: Verify the whole module compiles**

Run: `go build ./...`
Expected: no errors (`Necessary`/`NecessaryVolumes` signatures are unchanged, so no caller breaks).

- [ ] **Step 6: Lint and commit**

```bash
make fmt && make lint
git add internal/sandbox/runtimemigration/legacy.go internal/sandbox/runtimemigration/legacy_test.go
git commit -m "feat(migration): derive legacy necessity from migration status"
```

---

### Task 3: Locked copy campaign in `runtimemigration`

**Files:**
- Create: `internal/sandbox/runtimemigration/lock.go`
- Modify: `internal/sandbox/runtimemigration/copy.go`
- Modify: `internal/sandbox/runtimemigration/offer.go` (call-site update for the `CopyVolumes` arity change)
- Test: `internal/sandbox/runtimemigration/lock_test.go`, `internal/sandbox/runtimemigration/copy_test.go`, `internal/sandbox/runtimemigration/offer_test.go`

**Interfaces:**
- Consumes: Task 1 (`state.Migration`, `state.MigrationCopying/Completed`, `state.UpdateRootState`, `state.RootState.SetMigration`), Task 2 (`MigrationName`, `necessaryVolumesFor`), `state.FlockExclusive`, `runtime.PrivateHome`, `msb.Client`.
- Produces:
  - `func acquireMigrationLock() (func(), error)` — creates `$MSB_HOME/.agents-sandbox` (mode 0700), opens `migration.lock` (O_CREATE|O_RDWR, 0600), takes `state.FlockExclusive`; returns a release that closes the file. Flock auto-releases on process exit.
  - `func CopyVolumes(ctx context.Context, client msb.Client, ui termio.UI) error` — **signature change** (drops `names`).
  - `func saveMigration(m state.Migration) error`.
  - `func copyVolume(ctx context.Context, client msb.Client, legacyHome, name string) error` — no marker write; `discardIncompleteVolume` then `CreateVolume` then `copyTree`.
  - `copyCompleteMarker`, `markCopyComplete`, `writeMarkerFile` are deleted.

`CopyVolumes` flow: acquire lock; read state; if status terminal return nil; if not `copying`, set `copying` and persist; derive necessary under the lock; for each name, `copyVolume` → on success append to `Copied` and persist, on failure `removeVolumeBestEffort` + append to `Excluded` + persist + `ui.Warnf`; after the loop persist terminal `completed`.

- [ ] **Step 1: Write the failing lock test**

Create `internal/sandbox/runtimemigration/lock_test.go`:

```go
func TestAcquireMigrationLockBlocksSecondHolder(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	release, err := acquireMigrationLock()
	if err != nil {
		t.Fatalf("acquireMigrationLock: %v", err)
	}
	done := make(chan struct{})
	go func() { r, _ := acquireMigrationLock(); if r != nil { r() }; close(done) }()
	select {
	case <-done:
		t.Fatal("second acquire returned while the first held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
}
```

(The blocking assertion uses the real flock; the test holds the lock for 200ms.)

- [ ] **Step 2: Write the failing copy tests**

In `internal/sandbox/runtimemigration/copy_test.go`, delete the marker tests (`TestCopyVolumesWritesCompletionMarker`, `TestCopyVolumesReplacesIncompleteVolume`'s marker assertions, `TestCopyVolumesRemovesVolumeOnMarkerFailure`) and update all `CopyVolumes(..., []string{name}, ...)` calls to `CopyVolumes(ctx, client, ui)`. Ensure `configpaths.WithMockConfigPaths(t)` is called (CopyVolumes now reads/writes state). Add:

```go
func TestCopyVolumesRecordsProgressAndCompletes(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "legacy"
	addVolumeDir(t, legacy, name)
	writeFileWithMode(t, filepath.Join(legacy, "volumes", name, "f.txt"), "x", 0o600)
	target := t.TempDir()
	client := &msb.MockMsbClient{CreateVolumeFn: func(_ context.Context, n string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
		return &msb.MockVolumeHandle{Name_: n, Path_: target}, nil
	}}
	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, &ui); err != nil {
		t.Fatalf("CopyVolumes: %v", err)
	}
	root, _ := state.ReadRootState()
	m, _ := root.Migration(MigrationName)
	if m.Status != state.MigrationCompleted {
		t.Fatalf("status = %q, want completed", m.Status)
	}
	if len(m.Copied) != 0 || len(m.Excluded) != 0 {
		t.Fatalf("terminal migration kept transient lists: %+v", m)
	}
}

func TestCopyVolumesExcludesVolumeScopedFailureAndCompletes(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	bad := naming.HomePrefix + "bad"
	good := naming.HomePrefix + "good"
	addVolumeDir(t, legacy, bad)
	addVolumeDir(t, legacy, good)
	writeFileWithMode(t, filepath.Join(legacy, "volumes", good, "f.txt"), "x", 0o600)
	target := t.TempDir()
	client := &msb.MockMsbClient{CreateVolumeFn: func(_ context.Context, n string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
		if n == bad {
			return nil, errors.New("volume boom")
		}
		return &msb.MockVolumeHandle{Name_: n, Path_: target}, nil
	}}
	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, &ui); err != nil {
		t.Fatalf("CopyVolumes: %v", err)
	}
	root, _ := state.ReadRootState()
	m, _ := root.Migration(MigrationName)
	// Terminal status drops the transient lists (per spec: a campaign reaching
	// `completed` collapses to the status word). The exclusion itself is observed
	// mid-campaign in TestCopyVolumesRecordsExclusionBeforeCompleting.
	if m.Status != state.MigrationCompleted || len(m.Copied) != 0 || len(m.Excluded) != 0 {
		t.Fatalf("migration = %+v, want terminal completed with no transient lists", m)
	}
	if len(ui.WarnCalls) == 0 {
		t.Error("no warning for the excluded volume")
	}
}

func TestCopyVolumesResumesFromCopying(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	done := naming.HomePrefix + "done"
	partial := naming.HomePrefix + "partial"
	addVolumeDir(t, legacy, done)
	addVolumeDir(t, legacy, partial)
	// A prior campaign copied "done" then crashed; "partial" is present but unrecorded.
	if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
		MigrationName: {Status: state.MigrationCopying, Copied: []string{done}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	target := t.TempDir()
	var created []string
	client := &msb.MockMsbClient{CreateVolumeFn: func(_ context.Context, n string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
		created = append(created, n)
		return &msb.MockVolumeHandle{Name_: n, Path_: target}, nil
	}}
	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, &ui); err != nil {
		t.Fatalf("CopyVolumes: %v", err)
	}
	if len(created) != 1 || created[0] != partial {
		t.Fatalf("created = %v, want only %q (resume skips copied)", created, partial)
	}
}

func TestCopyVolumesDoesNotFollowCopiedSymlink(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "legacy"
	addVolumeDir(t, legacy, name)
	hostFile := filepath.Join(t.TempDir(), "host-secret")
	writeFileWithMode(t, hostFile, "keep", 0o600)
	// A guest could plant this exact name as a symlink to a host file.
	if err := os.Symlink(hostFile, filepath.Join(legacy, "volumes", name, ".agents-sandbox-copy-complete")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	target := t.TempDir()
	client := &msb.MockMsbClient{CreateVolumeFn: func(_ context.Context, n string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
		return &msb.MockVolumeHandle{Name_: n, Path_: target}, nil
	}}
	ui := termio.NewTestMock(t)
	if err := CopyVolumes(context.Background(), client, &ui); err != nil {
		t.Fatalf("CopyVolumes: %v", err)
	}
	got, err := os.ReadFile(hostFile)
	if err != nil || string(got) != "keep" {
		t.Fatalf("host file = %q, %v; want untouched", got, err)
	}
}
```

Also add a campaign-scoped failure test: make the state root unwritable (a file at `stateRoot()`) so the `copying`/`completed` write fails, and assert `CopyVolumes` returns an error.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/sandbox/runtimemigration/ -run 'TestAcquireMigrationLock|TestCopyVolumes' -v`
Expected: compile error — `acquireMigrationLock` undefined, `CopyVolumes` arity mismatch, marker symbols removed.

- [ ] **Step 4: Implement `lock.go`**

```go
package runtimemigration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

// acquireMigrationLock takes the host-only advisory lock that serializes the
// legacy home-volume copy campaign across processes. The flock auto-releases on
// process exit, so a crashed holder never pins the campaign. The returned
// function releases the lock.
func acquireMigrationLock() (func(), error) {
	dir := filepath.Join(runtime.PrivateHome(), ".agents-sandbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create migration lock dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "migration.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open migration lock: %w", err)
	}
	if err := state.FlockExclusive(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock migration lock: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
```

- [ ] **Step 5: Implement `copy.go`**

Delete `copyCompleteMarker`, `markCopyComplete`, `writeMarkerFile`. Implement:

```go
// saveMigration persists a migration entry via a read-modify-write so other
// registry entries survive.
func saveMigration(m state.Migration) error {
	return state.UpdateRootState(func(root *state.RootState) {
		root.SetMigration(MigrationName, m)
	})
}

func CopyVolumes(ctx context.Context, client msb.Client, ui termio.UI) error {
	release, err := acquireMigrationLock()
	if err != nil {
		return err
	}
	defer release()

	root, err := state.ReadRootState()
	if err != nil {
		return err
	}
	m, _ := root.Migration(MigrationName)
	if m.Status == state.MigrationCompleted || m.Status == state.MigrationDismissed {
		return nil
	}
	if m.Status != state.MigrationCopying {
		m = state.Migration{Status: state.MigrationCopying}
		if err := saveMigration(m); err != nil {
			return err
		}
	}

	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return err
	}
	names, err := necessaryVolumesFor(m)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := copyVolume(ctx, client, legacyHome, name); err != nil {
			removeVolumeBestEffort(ctx, client, name)
			m.Excluded = append(m.Excluded, name)
			if serr := saveMigration(m); serr != nil {
				return serr
			}
			ui.Warnf("skipping legacy home volume %q: %v", name, err)
			continue
		}
		m.Copied = append(m.Copied, name)
		if err := saveMigration(m); err != nil {
			return err
		}
		ui.Infof("copied legacy home volume %q", name)
	}
	return saveMigration(state.Migration{Status: state.MigrationCompleted})
}

func copyVolume(ctx context.Context, client msb.Client, legacyHome, name string) error {
	if err := discardIncompleteVolume(ctx, client, name); err != nil {
		return err
	}
	volume, err := client.CreateVolume(ctx, name, msbSdk.WithVolumeKind(msbSdk.VolumeKindDir))
	if err != nil {
		return fmt.Errorf("create volume %q: %w", name, err)
	}
	src := filepath.Join(legacyHome, "volumes", name)
	if err := copyTree(src, volume.Path()); err != nil {
		return fmt.Errorf("copy volume %q: %w", name, err)
	}
	return nil
}
```

- [ ] **Step 6: Update the `offer.go` call site for the new `CopyVolumes` arity**

`CopyVolumes` no longer takes `names`. In `offer.go`, change the `choiceCopy` branch's `CopyVolumes(ctx, client, names, ui)` to `CopyVolumes(ctx, client, ui)`. Leave the rest of `Offer` (the `names` derivation for the prompt, the terminal check from Task 1) intact; Task 4 rewrites the flow.

- [ ] **Step 7: Run the migration tests to verify they pass**

Run: `go test ./internal/sandbox/runtimemigration/ -v`
Expected: PASS.

- [ ] **Step 8: Verify the whole module compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 9: Lint and commit**

```bash
make fmt && make lint
git add internal/sandbox/runtimemigration/lock.go internal/sandbox/runtimemigration/copy.go internal/sandbox/runtimemigration/offer.go internal/sandbox/runtimemigration/lock_test.go internal/sandbox/runtimemigration/copy_test.go
git commit -m "feat(migration): copy legacy volumes under a host-only lock with exact resume"
```

---

### Task 4: Wire `Offer` to the registry

**Files:**
- Modify: `internal/sandbox/runtimemigration/offer.go`
- Test: `internal/sandbox/runtimemigration/offer_test.go`

**Interfaces:**
- Consumes: Task 1 (`state.Migration*`, `state.UpdateRootState`), Task 2 (`MigrationName`, `necessaryVolumesFor`), Task 3 (`CopyVolumes`, `saveMigration`).
- Produces: `Offer(ctx, client, ui)` unchanged signature.

Flow: read root state (best-effort warn+skip); terminal status → nil; derive necessary for the status; if none and status is `copying`, persist `completed`; if none otherwise → nil; non-interactive → `ErrMigrationPending`; on `choiceCopy` call `CopyVolumes`; on `choiceFresh` persist `MigrationDismissed`; on quit/failure → `ErrMigrationPending`.

- [ ] **Step 1: Write/update the failing tests**

In `internal/sandbox/runtimemigration/offer_test.go`, update `state.RootState{...}` constructions to the registry shape (`Migrations: map[string]state.Migration{MigrationName: {Status: state.MigrationDismissed}}}`) and read assertions to `root.Migration(MigrationName)`. Update `writeCorruptRootState`'s payload to a still-invalid YAML. Add:

```go
func TestOfferTerminalMigrationDoesNotPrompt(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")
	if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
		MigrationName: {Status: state.MigrationCompleted},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)
	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if prompted {
		t.Error("prompted after a terminal migration")
	}
}

func TestOfferFinalizesOpenCampaignWithNothingLeft(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "done"
	addVolumeDir(t, legacy, name)
	// Campaign copied everything, then crashed before writing completed.
	if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
		MigrationName: {Status: state.MigrationCopying, Copied: []string{name}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ui := &termio.Mock{IsInteractiveResult: true}
	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	root, _ := state.ReadRootState()
	m, _ := root.Migration(MigrationName)
	if m.Status != state.MigrationCompleted {
		t.Fatalf("status = %q, want completed", m.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/runtimemigration/ -run TestOffer -v`
Expected: compile error / assertion failures against the old `RuntimeHomeMigration` shape.

- [ ] **Step 3: Implement `offer.go`**

Rewrite the body per the flow above. `choiceFresh` uses `saveMigration(state.Migration{Status: state.MigrationDismissed})`. On `choiceCopy`, call `CopyVolumes(ctx, client, ui)` (no names). Keep the existing warning texts and `ErrMigrationPending` guidance.

- [ ] **Step 4: Run the full package tests**

Run: `go test ./internal/sandbox/runtimemigration/ -v`
Expected: PASS.

- [ ] **Step 5: Run the wider suite**

Run: `go build ./... && go test ./internal/sandbox/... -count=1`
Expected: PASS (confirms `vm.run_orchestrate` still links and the orchestration tests are green).

- [ ] **Step 6: Lint and commit**

```bash
make fmt && make lint
git add internal/sandbox/runtimemigration/offer.go internal/sandbox/runtimemigration/offer_test.go
git commit -m "feat(migration): drive the offer from the migration registry"
```

---

### Task 5: Docs and CHANGELOG

**Files:**
- Modify: `CHANGELOG.md`
- Modify: any `docs/` page describing the legacy copy (verify first)

**Interfaces:**
- Consumes: none.
- Produces: user-facing documentation matching the new state model.

- [ ] **Step 1: Find affected docs**

Run: `rg -n "runtime_home_migration|copy-complete|sentinel|legacy home" docs/ CHANGELOG.md`
Expected: CHANGELOG hits at the `[Unreleased]` "Runtime" bullets; docs hits only if a page documents the offer.

- [ ] **Step 2: Update `CHANGELOG.md` under `[Unreleased]`**

- Rewrite the "start fresh (persisted as `runtime_home_migration.dismissed: true`...)" bullet to describe the `migrations.runtime_home` registry (statuses `copying`/`completed`/`dismissed`, transient `copied`/`excluded`).
- Replace the `### Fixed` sentinel bullet ("marked with a sentinel file inside the volume") with the new guarantee: completion is host-owned (no in-volume file), the copy runs under a host-only lock, concurrent sessions cannot discard a live copy, and an interrupted campaign resumes exactly.
- Keep entries in the repo's style; do not add a new section.

- [ ] **Step 3: Update any affected `docs/` page**

If a page describes the copy's persisted state or the sentinel, update it to the registry model. Follow the docs linking rules in `AGENTS.md`. If no page is affected, note that in the commit message.

- [ ] **Step 4: Run the docs check and full check**

Run: `make check`
Expected: `ci/check-docs.sh` and its tests pass; all fmt/lint/test targets green.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md docs/
git commit -m "docs(migration): describe the host-owned migration registry"
```

---

## Self-Review

**Spec coverage:**
- Registry schema + terminal/transient split → Task 1.
- Status-aware necessity + `pending`-as-absence → Task 2, Task 4.
- Host-only flock, exact resume via `copied`, auto `excluded`, campaign-scoped abort → Task 3.
- Symlink safety / no in-volume completion → Task 3 (symlink test, marker removal).
- Terminal permanence / no re-arm → Task 4 (terminal test).
- Docs + CHANGELOG → Task 5.
- "Left to the builder" items (prompt hook location, `MSB_HOME` wiring, ownership verification) are unchanged by this plan and already handled outside it.

**Step scan:** Each step names a file and either the exact test assertions or the exact signature/flow. No "handle edge cases" placeholders.

**Type consistency:** `state.Migration`, `state.MigrationStatus`, `state.MigrationCopying/Completed/Dismissed`, `state.UpdateRootState`, `state.RootState.SetMigration`, `state.RootState.Migration`, `runtimemigration.MigrationName`, `necessaryVolumesFor`, `presentPrivateVolumeNames`, `saveMigration`, `acquireMigrationLock`, `CopyVolumes(ctx, client, ui)` are used with one spelling across tasks.

**Review Focus coverage:** concurrency → Task 3 flock test; crash resume → Task 3 resume test; exclusion → Task 3 exclusion test; symlink → Task 3 symlink test; terminal → Task 4 terminal test.

**Proportion:** ~5 tasks for a state-model + copy-path refactor across 5 source files; no function bodies beyond the state-machine logic the tests do not determine.
