# Runtime Management Slice 5 (Storage Accounting) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `df` storage report over the Slice 2 `internal/sandbox/storage` seam and a `storage` command group whose `df`/`prune` subcommands alias the top-level `df`/`prune`.

**Architecture:** `df` calls `storage.Observe` (Slice 2), then renders a category-totals table (or a structured JSON report), adding per-object rows and host paths under `--verbose`. The `storage` group is a plain Cobra parent reusing the exact `buildDFCmd`/`buildPruneCmd` builders, so the aliases cannot drift from the top-level commands.

**Tech Stack:** Go 1.26, Cobra, `termio` (UI/table/printer), `storage.Observe`, `humanize.FormatBytes`.

**Spec:** `.operator-shared/specs/runtime-management-commands.md` (§Slice 5, §"Command surface contract"). Ticket: #155 (child of #165). Depends on Slices 0 (#168, merged) and 2 (#170, merged).

## Global Constraints

- Follow `AGENTS.md`: idiomatic Go, non-abbreviated identifiers, `termio` for all output, small focused files, no comments unless non-obvious.
- Toolchain: `make fmt` / `make lint` (`golangci-lint`) — never `go vet` or hand-formatting; `make test`; `make compile-all` before finishing.
- Nil storage counters mean **unknown**, never zero. Human output renders nil as `-`; JSON emits `null`.
- `storage.Observe` degrades gracefully on `ErrUnsupportedOperation` (categories unknown + note); any other error propagates and fails the command.
- `--format json` and `--quiet` are mutually exclusive via the shared `resolveListOutput`.
- Scope to the private runtime home; never touch a user's `~/.microsandbox`.
- Do not commit or push except where a task says to commit; do not finalize the branch without the maintainer.

## Decisions locked for this slice

Confirmed with the user by grilling (2026-10-16), grounded in the real `msb storage df` source
(`crates/cli/lib/commands/storage.rs`, fetched from `superradcompany/microsandbox@main`):

1. **Align with `msb df`.** `df` mirrors msb's storage report, extending/reformatting only where it makes sense for agents-sandbox (the extra `runtime` category, graceful degradation instead of erroring, our global `-q`).
2. **Human table = msb's:** columns `TYPE COUNT IN USE LOGICAL SIZE RECLAIMABLE`; category order images → snapshots → branch memory → snapshot memory → sandboxes → volumes → runtime; non-verbose footer `logical sizes; '-' means unknown. use --verbose for details.`; cell rendering decimal/`-`, `humanize.FormatBytes`/`-`, `Yes`/`No`/`-`.
3. **`--verbose` = msb's item blocks** per category (title `Name (count)`, then per item `Name` / `Path:` / `Logical size:` / `Allocated:` / `In use:` / `Reclaimable:` / `Reason:` lines, then category notes as `name: note`), preceded by msb's reclaimable/physical-space note paragraph and the report's top-level notes.
4. **JSON mirrors msb's object shape** — category-keyed (`images`, `snapshots`, `branch_memory`, `snapshot_memory`, `sandboxes`, `volumes`, plus our `runtime`), each with `count`, `in_use`, `logical_bytes`, `allocated_bytes`, `reclaimable_logical_bytes`, `items` (always present, `[]` when empty), `notes`; plus top-level `notes`. Pretty-printed (matches msb and the shared `writeJSON`). Unknown counters are `null`.
5. **`--verbose` affects only the human render.** JSON always includes `items` (msb behaviour); the CLI sets `IncludeObjects` when `verbose || mode == listOutputJSON`.
6. **`-q/--quiet` prints the bare minimum like a list:** one display label per line (`images`, `snapshots`, `branch memory`, `snapshot memory`, `sandboxes`, `volumes`, `runtime`). `--format json` + `--quiet` is still mutually exclusive.
7. **Graceful degradation (Q10b):** on `ErrUnsupportedOperation` the seam records a top-level note and leaves categories unknown; the non-verbose table still renders with an extra note line explaining the all-`-` output (msb itself errors). Any other error propagates.
8. **Seam alignment (Q9a):** `storage` category names become snake_case (`branch_memory`, `snapshot_memory`), reordered to msb order; the CLI derives display labels by replacing `_` with space. `Totals` gains `InUse *uint64` (mapped from msb's category `in_use`); the `runtime` category leaves it nil.
9. **`storage prune` shares `buildPruneCmd`** (confirmation, `--yes`, `--older-than`, `--format json` all inherited). `prune` gains the `also: storage prune` tree annotation (Q6a).

## Review Focus

1. `--format json` + `--quiet` → mutually exclusive (Task 1).
2. Unknown (nil) counters → human `-`, JSON `null`; a missing runtime subtree is a known zero, not unknown (Tasks 1).
3. Human output matches `msb df` shape (columns, order, footers, verbose item blocks) with `runtime` appended; JSON matches msb's category-keyed object (Task 1).
4. `ErrUnsupportedOperation` → no error, unknown categories + one top-level note; other errors fail the command (Task 1).
5. `storage` category names are snake_case in the seam and display labels in the CLI (`storageDisplayName`); `git grep` finds no stale `branch-memory`/`snapshot-memory` literals (Task 1).
6. `storage df` output equals top-level `df`; `storage prune` is gated by confirmation just like top-level `prune` (Task 2).

---

### Task 1: `df` command

**Files:**
- Create: `cmd/agents-sandbox/commands_storage.go`
- Create: `cmd/agents-sandbox/storage_output.go`
- Create: `cmd/agents-sandbox/cli_storage_test.go`
- Modify: `cmd/agents-sandbox/constants.go` (`cmdDF`, `cmdStorage`, `flagVerbose`)
- Modify: `cmd/agents-sandbox/commands.go` (register `df`)

**Interfaces:**
- Modifies (seam): `internal/sandbox/storage` — snake_case category names, msb order, `Totals.InUse`, top-level `Report.Notes`; `policy: storage` still owns the category list.
- Produces:
  - `buildDFCmd(ui termio.UI) *cobra.Command` — `df [--verbose] [--format json]`, `cobra.NoArgs`, annotation `also: storage df`.
  - `printStorageReport(ui termio.UI, report *storage.Report, verbose bool)` — msb-shaped human table / item blocks.
  - `newStorageReportJSON(report *storage.Report) jsonStorageUsage` — msb-shaped object.
  - Helpers `formatOptionalUint(*uint64) string`, `formatOptionalBytes(*uint64) string`, `formatOptionalBool(*bool) string`, `storageDisplayName(string) string`.
- Consumes: `storage.Observe`, `storage.Options{IncludeObjects}`, `resolveListOutput`, `writeJSON`, `humanize.FormatBytes`, `ui.NewTable`.

**JSON DTO (msb-shaped):**

```go
type jsonStorageUsage struct {
    Images         jsonStorageCategory `json:"images"`
    Snapshots      jsonStorageCategory `json:"snapshots"`
    BranchMemory   jsonStorageCategory `json:"branch_memory"`
    SnapshotMemory jsonStorageCategory `json:"snapshot_memory"`
    Sandboxes      jsonStorageCategory `json:"sandboxes"`
    Volumes        jsonStorageCategory `json:"volumes"`
    Runtime        jsonStorageCategory `json:"runtime"`
    Notes          []string            `json:"notes"`
}
type jsonStorageCategory struct {
    Count                   *uint64           `json:"count"`
    InUse                   *uint64           `json:"in_use"`
    LogicalBytes            *uint64           `json:"logical_bytes"`
    AllocatedBytes          *uint64           `json:"allocated_bytes"`
    ReclaimableLogicalBytes *uint64           `json:"reclaimable_logical_bytes"`
    Items                   []jsonStorageItem `json:"items"`
    Notes                   []string          `json:"notes"`
}
type jsonStorageItem struct {
    Name           string   `json:"name"`
    Path           string   `json:"path"`
    LogicalBytes   *uint64  `json:"logical_bytes"`
    AllocatedBytes *uint64  `json:"allocated_bytes"`
    InUse          *bool    `json:"in_use"`
    Reclaimable    *bool    `json:"reclaimable"`
    Reasons        []string `json:"reasons"`
}
```

- [x] **Step 1: Write the failing CLI tests** (written; being reworked to msb shape)

`cli_storage_test.go`, using `setupCommandFixtures` + `sandboxmsb.WithMsbMock` + `StorageUsageFn`:

- `TestDF`: mock all six categories with counts/bytes; assert `ui.OutCalls` contains the header `TYPE COUNT IN USE LOGICAL SIZE RECLAIMABLE` and rows in msb order (e.g. `images 2 - 100 B -`, `branch memory 5 - 50 B -`, `runtime 0 - 0 B -`), and the non-verbose footer.
- `TestDFFormatJSON`: assert msb-shaped keys (`images`, `snapshots`, `branch_memory`, `snapshot_memory`, `sandboxes`, `volumes`, `runtime`, `notes`); `in_use` null when unknown; `items` an empty array; unknown counters `null`.
- `TestDFVerbose`: create a runtime file under the private home; assert the item block (`Name`, `Path:`, `Logical size:`) and the reclaimable/physical note paragraph.
- `TestDFUnsupported`: `&msbSdk.Error{Kind: msbSdk.ErrUnsupportedOperation}` → no error, table all `-`, and the top-level note line appears (non-verbose).
- `TestDFQuiet`: `-q` prints display labels one per line (`images`, `branch memory`, …, `runtime`) via `ui.PrintCalls`.
- `TestDFFormatConflict` / `TestDFFormatUnknown`: `runListCmdTest` with `[]string{cmdDF, ...}` and `"mutually exclusive"` / `"unsupported format"`.
- `TestDFObserveError`: `StorageUsageFn` returns `errors.New("boom")` → command error containing `observe storage`.
- `TestDFNoArgs`: extra positional arg errors.

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run TestDF -v`
Expected: FAIL — `cmdDF`/`buildDFCmd` undefined.

- [ ] **Step 3: Add constants**

`constants.go`: `cmdDF = "df"`, `cmdStorage = "storage"`, `flagVerbose = "verbose"` in the const block.

- [x] **Step 4: Implement `storage_output.go`** (done; rework to msb shape)

Human non-verbose: `ui.NewTable("TYPE", "COUNT", "IN USE", "LOGICAL SIZE", "RECLAIMABLE")`, one row per category in seam order with `storageDisplayName(name)`; then `ui.Out("logical sizes; '-' means unknown. use --verbose for details.")`; then each `report.Notes` line (the graceful-degradation note).
Human verbose: `ui.Out("")`, the reclaimable/physical-space paragraph, top-level notes, then per category a blank line, `ui.Out("  " + title + " (" + count + ")")` (title capitalised), and per item the `Name`/`Path:`/`Logical size:`/`Allocated:`/`In use:`/`Reclaimable:`/`Reason:` lines, then category notes as `  name: note`.
JSON: `newStorageReportJSON` maps the seven categories to the msb-shaped object (items always `[]`, notes always `[]`).

- [x] **Step 5: Implement `commands_storage.go` (`buildDFCmd`)** (done; rework)

Resolve mode via `resolveListOutput(format, ui.Quiet())`; `IncludeObjects := verbose || mode == listOutputJSON`; call `storage.Observe(cmd.Context(), storage.Options{IncludeObjects: includeObjects})`, wrapping errors with `fmt.Errorf("observe storage: %w", err)`; then JSON → `writeJSON`, table → `printStorageReport`, identifiers → `ui.Print(storageDisplayName(c.Name))` per category.

- [ ] **Step 6: Register `df` in `commands.go`**

Add `rootCmd.AddCommand(buildDFCmd(ui))` next to `buildPruneCmd`.

- [ ] **Step 7: Run the tests and confirm they pass**

Run: `go test ./cmd/agents-sandbox/ -run TestDF -v && make fmt && make lint`
Expected: PASS, no lint findings.

- [ ] **Step 8: Commit**

```bash
git add cmd/agents-sandbox
git commit -m "feat(cli): df storage report with --verbose and --format json (#155)"
```

---

### Task 2: `storage` group alias

**Files:**
- Modify: `cmd/agents-sandbox/commands_storage.go` (`buildStorageCmd`)
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildPruneCmd` annotation)
- Modify: `cmd/agents-sandbox/commands.go` (register `storage`)
- Modify: `cmd/agents-sandbox/cli_storage_test.go`, `cmd/agents-sandbox/cli_tree_test.go`

**Interfaces:**
- Produces: `buildStorageCmd(ui termio.UI) *cobra.Command` — parent of `buildDFCmd(ui)` and `buildPruneCmd(ui)`.

- [ ] **Step 1: Write the failing tests**

- `TestStorageDF`: `storage df` renders the same totals as `df` for the same mock.
- `TestStoragePruneNonInteractiveRequiresYes`: `storage prune` without `--yes` → `termio.ErrConfirmationRequired`.
- `TestStoragePruneDryRun`: `storage prune --dry-run` succeeds and prints a report (mirrors top-level prune).
- `TestTreeContainsStorageAndDF`: tree output contains `df` and `storage` (extend the existing `cli_tree_test.go` list).

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'TestStorage|TestTree' -v`
Expected: FAIL — `cmdStorage`/`buildStorageCmd` undefined.

- [ ] **Step 3: Implement `buildStorageCmd` and register it**

`buildStorageCmd` builds a `cobra.Command{Use: cmdStorage, Args: cobra.NoArgs, Short: "Inspect and reclaim runtime storage"}` and adds `buildDFCmd(ui)` + `buildPruneCmd(ui)`. Register `buildStorageCmd(ui)` in `commands.go`. Add `Annotations: map[string]string{annotationAlsoAs: "storage prune"}` to `buildPruneCmd` so the tree shows the alias.

- [ ] **Step 4: Run and confirm pass**

Run: `go test ./cmd/agents-sandbox/ -run 'TestStorage|TestTree' -v && make fmt && make lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/agents-sandbox
git commit -m "feat(cli): storage group aliasing df and prune (#155)"
```

---

### Task 3: Docs, CHANGELOG, spec

**Files:**
- Modify: `docs/commands.md` (new `df`/`storage` section)
- Modify: `CHANGELOG.md` (`[Unreleased]` → Added)
- Modify: `.operator-shared/specs/runtime-management-commands.md` (mark Slice 5 implemented; record the JSON/quiet decisions)
- Modify: `.operator-shared/catalog.md` (plan entry)

- [ ] **Step 1: Docs** — document `df` (`--verbose`, `--format json`, `-q` suppression) and `storage df`/`storage prune`, matching the existing prune/list section style.
- [ ] **Step 2: CHANGELOG** — Added: `df` storage report and the `storage` group.
- [ ] **Step 3: Spec** — flip Slice 5 status to implemented and add resolved decisions (raw JSON counters, quiet suppression, `--verbose` objects).
- [ ] **Step 4: Verify** — `make check`.
- [ ] **Step 5: Commit** — `docs: record runtime-management Slice 5 (#155)`.

---

## Self-review notes

- **Spec coverage:** `df [--verbose] [--format json]` (T1), `storage df`/`storage prune` + top-level `df`/`prune` (T2), docs/CHANGELOG/spec (T3).
- **Type consistency:** `buildDFCmd`/`buildPruneCmd` reused verbatim for the group; `jsonStorage*` types defined once.
- **Not in scope:** Slice 4 removal commands (#172) and runtime-RAM pruning (#156) are separate.
