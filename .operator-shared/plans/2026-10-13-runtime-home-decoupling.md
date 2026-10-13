# Runtime Home Decoupling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give agents-sandbox its own private microsandbox runtime home, fully decoupled from any user-installed `microsandbox`, with an opt-in copy of legacy home volumes.

**Architecture:** agents-sandbox unconditionally points `MSB_HOME` at `$XDG_DATA_HOME/agents-sandbox/msb` and provisions the SDK-pinned runtime there. The shared-runtime concepts (external `MSB_PATH`/`MSB_LIBKRUNFW_PATH`, persisted paths, and the version-mismatch recovery menu) are removed; a runtime mismatch becomes a plain error. Existing users keep working because a recorded home volume that no longer resolves simply yields a fresh one. A one-time prompt on session start offers to copy legacy `agents-sandbox-home-*` volumes into the private home; declining is remembered as `runtime_home_migration.dismissed` in root `state.yaml`.

**Tech Stack:** Go 1.26, Cobra, Viper, `microsandbox` Go SDK (`sdk/go@v0.7.7`), `gopkg.in/yaml.v3`, testify.

**Spec:** `docs/superpowers/specs/2026-10-13-runtime-home-decoupling-design.md` (also committed at `.operator-shared/specs/runtime-home-decoupling.md`). Read it alongside this plan.

## Global Constraints

- The private runtime home is **unconditional**: `MSB_HOME` is set to `$XDG_DATA_HOME/agents-sandbox/msb` (default `~/.local/share/agents-sandbox/msb`) before any SDK call. No `MSB_PATH`, `MSB_LIBKRUNFW_PATH`, or persisted runtime paths; the `External`/`Incomplete` runtime concepts are deleted.
- agents-sandbox installs and advertises **no user-facing `msb`**.
- The runtime version-mismatch **recovery menu is deleted**; a mismatch is a plain error.
- The copy prompt runs **only on interactive session start (`run`/`shell`)**; `--yes` and non-interactive runs print a one-line reminder and never copy.
- The copy offer is **global** (one acceptance copies all necessary volumes) and **permanent once dismissed**.
- Persisted state is exactly `runtime_home_migration.dismissed: true` in root `~/.local/state/agents-sandbox/state.yaml`.
- Legacy source is `~/.microsandbox/volumes/<name>` for names matching `agents-sandbox-home-*`; only home volumes are copied, never binaries, caches, DB, or VM records.
- Style: use `golangci-lint` (`make fmt`, `make lint`), never `go fmt`/`go vet`. Run `make check` before finalizing.

## Review Focus

Most likely to bite a real user; each gets a test in its owning task:

1. Fresh install with no legacy `~/.microsandbox` — no prompt, no error. (Task 6/8)
2. Legacy home present but `volumes/` missing or unreadable — treated as not necessary, no error. (Task 6)
3. Non-interactive run or `--yes` with necessary volumes — reminder only, no copy, no hang. (Task 8)
4. Existing user whose recorded home volume is absent from the private home — starts fresh, no error. (Task 9)
5. Installed `msb` version differs from the SDK version — plain, actionable error; no menu, no hang. (Task 3)

---

### Task 1: `configpaths.UserDataDir()`

**Files:**
- Modify: `internal/configpaths/configpaths.go:10-26` (interface), `:38-52` (real impl)
- Modify: `internal/configpaths/testmock.go:52-62`, `:107` (mock + failfast)
- Test: `internal/configpaths/config_paths_test.go`

**Interfaces:**
- Produces: `ConfigPaths.UserDataDir() string` — `$XDG_DATA_HOME/agents-sandbox`, defaulting to `~/.local/share/agents-sandbox`.

- [ ] **Step 1: Write the failing test** (append to `config_paths_test.go`)

```go
func TestUserDataDirHonorsXDGDataHome(t *testing.T) {
	configpaths.WithRealConfigPaths(t)
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	want := filepath.Join("/tmp/xdg-data", "agents-sandbox")
	if got := configpaths.Get().UserDataDir(); got != want {
		t.Errorf("UserDataDir() = %q, want %q", got, want)
	}
}

func TestUserDataDirDefaultsToLocalShare(t *testing.T) {
	configpaths.WithRealConfigPaths(t)
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "agents-sandbox")
	if got := configpaths.Get().UserDataDir(); got != want {
		t.Errorf("UserDataDir() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/configpaths/ -run TestUserDataDir -v`
Expected: FAIL — `UserDataDir` undefined.

- [ ] **Step 3: Implement** in `configpaths.go` — add `UserDataDir() string` to the interface and to `realConfigPaths`:

```go
func (c *realConfigPaths) UserDataDir() string {
	return filepath.Join(xdgBaseDir("XDG_DATA_HOME", ".local/share"), pathPrefix)
}
```

Add the mock method in `testmock.go` (same pattern as `UserStateDir`), returning `ensureMockConfigDirectory(filepath.Join(m.baseDir, "userdata"))`, and the failfast method (`f.mustMock(); return ""`).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/configpaths/ -run TestUserDataDir -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/configpaths/
git commit -m "feat(configpaths): add UserDataDir"
```

---

### Task 2: Private runtime home + drop external resolution

**Files:**
- Create: `internal/sandbox/runtime/home.go`, `internal/sandbox/runtime/home_test.go`
- Modify: `internal/sandbox/runtime/runtime.go` (`selectRuntime` `:195-261`, `runtimeHome` `:581-590`; delete `persistedRuntimeConfig`/`persistedRuntimeConfigPaths` `:60-68`, `selectedRuntime.External/Incomplete` `:70-76`, `runtimeConfigPath`, `loadPersistedRuntimeConfig`, `runtimeLibraryCandidate`)
- Modify: `internal/sandbox/runtime/runtime_test.go` (drop tests for removed symbols)

**Interfaces:**
- Consumes: `configpaths.Get().UserDataDir()` (Task 1).
- Produces:
  - `func PrivateHome() (string, error)` — `filepath.Join(UserDataDir(), "msb")`.
  - `func LegacyHome() (string, error)` — `filepath.Join($HOME, ".microsandbox")`.
  - `func ApplyHome() (string, error)` — computes `PrivateHome()`, `os.Setenv("MSB_HOME", home)`, returns it.
  - `selectedRuntime{ Home, MSBPath, LibkrunfwPath string }` (no `External`/`Incomplete`).

- [ ] **Step 1: Write failing tests** in `home_test.go`

```go
func TestPrivateHomeUsesDataDir(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	got, err := PrivateHome()
	if err != nil { t.Fatal(err) }
	if filepath.Base(filepath.Dir(got)) != "agents-sandbox" || filepath.Base(got) != "msb" {
		t.Errorf("PrivateHome() = %q", got)
	}
}

func TestApplyHomeSetsMSBHome(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	home, err := ApplyHome()
	if err != nil { t.Fatal(err) }
	if os.Getenv("MSB_HOME") != home {
		t.Errorf("MSB_HOME = %q, want %q", os.Getenv("MSB_HOME"), home)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/sandbox/runtime/ -run 'TestPrivateHome|TestApplyHome' -v` → FAIL (undefined).

- [ ] **Step 3: Implement `home.go`** with the three functions above using `configpaths.Get().UserDataDir()` and the existing `userHomeDir` seam; then simplify `runtime.go`: make `runtimeHome()` return `PrivateHome()`; rewrite `selectRuntime()` to:

```go
func selectRuntime() (selectedRuntime, error) {
	home, err := PrivateHome()
	if err != nil { return selectedRuntime{}, err }
	return selectedRuntime{
		Home:          home,
		MSBPath:       filepath.Join(home, "bin", msbFilename()),
		LibkrunfwPath: filepath.Join(home, "lib", libkrunfwFilename()),
	}, nil
}
```

Delete the removed types/functions and their tests.

- [ ] **Step 4: Run to verify pass** — same command → PASS; then `go test ./internal/sandbox/runtime/`.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/runtime/
git commit -m "feat(runtime): private MSB home, drop external runtime resolution"
```

---

### Task 3: Retire the version-mismatch recovery menu

**Files:**
- Modify: `internal/sandbox/runtime/runtime.go` (`PrepareRuntime` `:330`, `prepareRuntime` `:337`, delete `recoverRuntimeMismatch`/`mismatchChoices`/`mismatchPrompt`/`nonInteractiveMismatchError`/`showIssueReport`/`completeRuntimePreparation`/`PreparationResult` `:80-82`; delete seam vars `runMSBDowngrade` `:99`, `latestAgentsSandboxVersion`/`updateAgentsSandbox` `:105-106`)
- Modify: `cmd/agents-sandbox/commands.go:147-153`
- Modify: `cmd/agents-sandbox/cli_test.go` (remove `TestRootCommandReturnsRuntimeRestart`)
- Modify: `internal/sandbox/runtime/runtime_test.go` (delete recovery tests listed in the exploration; `runtime.go:387-509` behavior)

**Interfaces:**
- Produces: `func PrepareRuntime(ctx context.Context, ui termio.UI, launcherVersion string) error` — calls `ApplyHome()`, inspects, installs if missing, validates, and returns a plain error on any non-`Equal` relation.
- Consumes: `PrivateHome`/`ApplyHome` (Task 2), existing `inspectRuntimeFunc`, `ensureRuntime`, `validatePreparedRuntime`, `sandboxmsb.SkipRuntimeInstall`.

- [ ] **Step 1: Write failing tests** (replace recovery tests)

```go
func TestPrepareRuntimeMismatchIsPlainError(t *testing.T) {
	// install runtime files with a different version at the private home
	// inspect -> RuntimeOlder; assert PrepareRuntime returns an error and
	// never touches ui.Select.
}

func TestPrepareRuntimeMissingInstallsAndValidates(t *testing.T) { /* ensure+validate called once, error nil */ }

func TestPrepareRuntimeEqualValidates(t *testing.T) { /* no install, validate called */ }
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/sandbox/runtime/ -run TestPrepareRuntime -v` → FAIL.

- [ ] **Step 3: Implement** — `PrepareRuntime` becomes:

```go
func PrepareRuntime(ctx context.Context, ui termio.UI, launcherVersion string) error {
	if !runtimeClientIsReal() { return nil }
	return prepareRuntime(ctx, ui, launcherVersion)
}
```

`prepareRuntime` mutex-guards on `runtimeState.prepared`, calls `ApplyHome()`, `inspectRuntimeFunc(msbSdk.SDKVersion())`, then:

```go
switch inspection.Relation {
case RuntimeEqual:
	validate else error; sandboxmsb.SkipRuntimeInstall(); prepared = true; return nil
case RuntimeMissing:
	ensureRuntime(ctx) else error; validatePreparedRuntime(ctx) else error; Skip; prepared; return nil
default:
	return runtimeMismatchError(inspection, "runtime is not the SDK-pinned version; reinstall agents-sandbox runtime")
}
```

Delete the menu helpers and now-unused seams. In `commands.go`, replace the `prepared.Restart` block with a plain `return err`; drop the `sandbox.ExitError` import if unused.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/sandbox/runtime/ ./cmd/agents-sandbox/ -run 'Runtime|RootCommand'` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/runtime/ cmd/agents-sandbox/
git commit -m "refactor(runtime): replace mismatch recovery menu with a plain error"
```

---

### Task 4: Remove the user-facing msb from doctor

**Files:**
- Modify: `internal/sandbox/doctor/install.go` (delete `pathHints`), `internal/sandbox/doctor/doctor.go:54-66` (`checkMsb`)
- Modify: `internal/sandbox/doctor/*_test.go` (drop path-hint assertions)

**Interfaces:**
- Consumes: `runtime.PrivateHome()` (Task 2) for reporting the installed location.
- Produces: `checkMsb` ensures installation and reports the private-home bin dir; no shell-rc/PATH hints.

- [ ] **Step 1: Write/adjust failing test** — assert doctor output contains the private-home bin path and contains **no** `PATH`/`shellRc`/symlink hint text.
- [ ] **Step 2: Run to verify failure** — `go test ./internal/sandbox/doctor/ -v`.
- [ ] **Step 3: Implement** — delete `pathHints` and its call; keep `ensureMsbInstalled`; report the path under `runtime.PrivateHome()`.
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Commit** — `git commit -m "refactor(doctor): stop advertising a user-facing msb"`

---

### Task 5: Root `state.yaml` accessor

**Files:**
- Create: `internal/sandbox/state/root.go`, `internal/sandbox/state/root_test.go`

**Interfaces:**
- Produces:
  - `type RootState struct { RuntimeHomeMigration RuntimeHomeMigration `yaml:"runtime_home_migration,omitempty"` }`
  - `type RuntimeHomeMigration struct { Dismissed bool `yaml:"dismissed,omitempty"` }`
  - `func ReadRootState() (*RootState, error)` — missing file returns a zero `RootState`; parse errors returned.
  - `func WriteRootState(s RootState) error` — atomic temp-rename (`0o600`, dir `0o700`), mirroring `vm/upgrade_state.go:127`.

- [ ] **Step 1: Write failing test** — round-trip `Dismissed: true`, and a missing file yields `Dismissed: false`.
- [ ] **Step 2: Run to verify failure** — `go test ./internal/sandbox/state/ -run RootState -v`.
- [ ] **Step 3: Implement** `root.go` with `rootStateFile() = filepath.Join(UserStateDir(), "state.yaml")`.
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Commit** — `git commit -m "feat(state): root state.yaml with runtime_home_migration key"`

---

### Task 6: Necessity derivation

**Files:**
- Create: `internal/sandbox/runtimemigration/legacy.go`, `.../legacy_test.go`

**Interfaces:**
- Consumes: `runtime.LegacyHome`, `runtime.PrivateHome` (Task 2), `naming.HomePrefix`.
- Produces:
  - `func legacyVolumeNames(legacyHome string) ([]string, error)` — basenames of dirs under `<legacyHome>/volumes` with prefix `naming.HomePrefix`; missing/unreadable dir → empty, nil.
  - `func privateVolumeNames(privateHome string) ([]string, error)` — same for `<privateHome>/volumes`.
  - `func necessaryVolumes(legacyHome, privateHome string) ([]string, error)` — legacy names absent from private set.
  - `func Necessary() (bool, error)` / `func NecessaryVolumes() ([]string, error)` — resolve homes via the `runtime` helpers.

- [ ] **Step 1: Write failing tests** for: empty legacy → none; legacy-only name → necessary; name present in both → not; legacy `volumes/` missing → none, no error (**Review Focus 2**); non-matching names ignored.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** using `os.ReadDir` + `os.IsNotExist` tolerance.
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Commit** — `git commit -m "feat(migration): derive necessary legacy home volumes"`

---

### Task 7: Copy legacy home volumes

**Files:**
- Create: `internal/sandbox/runtimemigration/copy.go`, `.../copy_test.go`

**Interfaces:**
- Consumes: `msb.Client` (`CreateVolume(ctx, name, msbSdk.WithVolumeKind(msbSdk.VolumeKindDir))` → `VolumeHandle.Path()`), `runtime.LegacyHome`, `termio.UI`.
- Produces:
  - `func copyTree(src, dst string) error` — recursive copy preserving file mode and modtime.
  - `func CopyVolumes(ctx context.Context, client msb.Client, names []string, ui termio.UI) error` — for each name create a `VolumeKindDir` volume in the private home and `copyTree(<legacyHome>/volumes/<name>, vol.Path())`.

- [ ] **Step 1: Write failing tests** — `copyTree` on a temp tree preserves contents/mode; `CopyVolumes` with `msb.MockMsbClient` (extend the mock's volume handle to return a temp `Path()` if needed) lands files in the target dir.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement.** Ownership: the copy is host-side as the invoking user; if a mapping to the in-VM `dev` user is required, resolve it here (verify with the session integration test) — do not shell out to any `msb`.
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Commit** — `git commit -m "feat(migration): copy legacy home volumes into the private home"`

---

### Task 8: Prompt and dismissal at session start

**Files:**
- Create: `internal/sandbox/runtimemigration/offer.go`, `.../offer_test.go`
- Modify: `internal/sandbox/vm/run_orchestrate.go:107-110` (call `Offer` after `msb.Get()`, before `resolveHomeVolume`)

**Interfaces:**
- Consumes: `Necessary`/`NecessaryVolumes`, `CopyVolumes`, `state.ReadRootState`/`WriteRootState`.
- Produces: `func Offer(ctx context.Context, client msb.Client, ui termio.UI) error`:

```go
// read root state; if dismissed -> return nil
// n, err := NecessaryVolumes(); if err -> err; if len(n)==0 -> return nil
// if !ui.IsInteractive() { ui.Warnf("legacy microsandbox home volumes can be copied; run in an interactive session"); return nil }
// key := ui.Select("Copy N legacy home volumes into agents-sandbox?", choices, "q")
//   "c" -> CopyVolumes(...); on success return nil
//   "s" -> WriteRootState(RootState{RuntimeHomeMigration{Dismissed: true}})
//   "q"/err -> return nil (offer again next time)
```

- [ ] **Step 1: Write failing tests** — dismissed → no prompt; nothing necessary → no prompt; non-interactive with necessary → warning, no copy (**Review Focus 3**); `--yes`/non-interactive treated identically (assert no `Select`); choose copy → `CopyVolumes` called; choose start-fresh → `Dismissed` persisted; choose later → no state change.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** `offer.go` and wire the call into `PrepareSandbox`.
- [ ] **Step 4: Run to verify pass** — `go test ./internal/sandbox/runtimemigration/ ./internal/sandbox/vm/`.
- [ ] **Step 5: Commit** — `git commit -m "feat(migration): offer legacy home-volume copy on session start"`

---

### Task 9: Pin the clean-break stale-state contract

**Files:**
- Test: `internal/sandbox/volume/state_test.go` (extend)

**Interfaces:**
- Consumes: `Manager.ResolveHomeVolume` (`volume/state.go:18`).

- [ ] **Step 1: Write the test** — record a home volume name in state, mock `GetVolume` to fail (or omit the volume), assert `ResolveHomeVolume` creates a fresh volume and rewrites state, returning no error (**Review Focus 4**).
- [ ] **Step 2: Run** — if it already passes (the code at `state.go:33-36` handles it), keep it as a regression pin; if it fails, fix the missing path in `ResolveHomeVolume`.
- [ ] **Step 3: Commit** — `git commit -m "test(volume): pin fresh-home fallback for missing recorded volume"`

---

### Task 10: Docs, CHANGELOG, spec status

**Files:**
- Modify: `docs/troubleshooting.md:15-42` (replace the shared-runtime mismatch/recovery story with the private-home behavior)
- Modify: `docs/` pages referencing `~/.microsandbox`, `MSB_PATH`, or installing `msb` (grep first)
- Modify: `CHANGELOG.md` (`[Unreleased]`)
- Modify: `.operator-shared/specs/runtime-home-decoupling.md` (mark decided points; link this plan) and `.operator-shared/catalog.md` if the Read If changes
- Test: `ci/check-docs.sh` via `make docs-linkcheck`

- [ ] **Step 1: Update docs** per the repo's linking rules (root docs use rendered HTML links; `docs/` pages use `{% link %}`).
- [ ] **Step 2: Add the CHANGELOG line.**
- [ ] **Step 3: Verify** — `make check` (fmt, lint, test, compile-all, docs). Expected: PASS.
- [ ] **Step 4: Commit** — `git commit -m "docs: private runtime home decoupling"`

---

## Self-Review

- **Spec coverage:** private home (T2), no user-facing msb (T4), retire recovery (T3), clean-break safety (T9), opt-in copy (T6–T8), persisted dismissal naming (T5), offer global/permanent/interactive-only (T8), docs (T10). No external escape hatch (T2 deletes it).
- **Type consistency:** `PrivateHome`/`LegacyHome`/`ApplyHome` (T2) are consumed unchanged in T4/T6–T8; `RootState.RuntimeHomeMigration.Dismissed` (T5) is the field written in T8; `CopyVolumes`/`NecessaryVolumes`/`Offer` signatures match their call sites.
- **Review Focus:** each of the five lines has an owning test (T6, T6, T8, T9, T3).
- **Proportion:** plan is comparable in size to the spec; bodies appear only where the algorithm is not determined (`selectRuntime`, `prepareRuntime`, `Offer`).
