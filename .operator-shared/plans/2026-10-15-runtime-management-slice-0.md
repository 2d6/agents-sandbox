# Runtime Management Slice 0 (Shared CLI Conventions) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the cross-cutting CLI conventions (list output modes, destructive confirmation, age alias, prune reports) that every later runtime-management command relies on, and wire the existing list/prune commands onto them.

**Architecture:** A shared list renderer selects table / identifiers-only / JSON from `--format` and the global `--quiet`. A `termio.UI` confirmation helper distinguishes `--yes` (skip prompt) from `--force` (override a guard). Prune reports become returned values rendered by the CLI, so the same report can be emitted as a human summary or JSON.

**Tech Stack:** Go 1.26, Cobra, `termio` (UI/table/printer), `pruning` package.

**Spec:** `.operator-shared/specs/runtime-management-commands.md` (§Slice 0, §"Cross-cutting conventions"). Ticket: #168 (child of #165).

## Global Constraints

- Follow `AGENTS.md`: idiomatic Go, non-abbreviated identifiers, `termio` for all output, small focused files.
- Toolchain: `make fmt` / `make lint` (`golangci-lint`) — never `go vet` or hand-formatting; `make test`; `make compile-all` before finishing.
- Tables remain the default list output; `--format json` and `--quiet` are mutually exclusive.
- All list/prune behavior is scoped to the private runtime home; never touch a user's `~/.microsandbox`.
- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- Do not commit or push except where a task says to commit; do not finalize the branch without the maintainer.

## Decisions locked with the maintainer

1. **`-q/--quiet` reuses the existing global flag.** On list commands it means *identifiers only* (like `docker ps -q`); elsewhere it keeps today's "suppress stdout" behavior. `sandbox list --names` is retained as a **deprecated alias** for it (hidden from help, warns, still honored).
2. **Prune confirmation is wired now.** Interactive prune prompts (default: no). `--yes` skips the prompt. Non-interactive without `--yes` **fails fast** with a clear error (never prompts or hangs).
3. **Prune `--format json`** emits a structured report object on stdout, replacing the human summary line.
4. **`--older-than` is canonical; `--age` is deprecated.** `--age`/`-a` stays functional (honored when set) but is marked deprecated via Cobra so it is hidden from help and warns when used.

## Review Focus

Inputs/conditions the spec implies but the happy-path tests do not exercise — each gets the named test in its task:

1. `--format json` together with `--quiet` — must fail as mutually exclusive, not silently pick one (Task 1).
2. An empty list under `--format json` — must emit `[]`, never `null` (Tasks 1–2).
3. `--quiet` list output in a **real** (non-mock) printer — the global quiet suppresses stdout, so identifiers must bypass that suppression (Task 1).
4. Non-interactive prune without `--yes` — must return an error, never block on stdin (Task 6).
5. `--older-than` with an invalid value — same parse error as before; both `--older-than` and `--age` given together is an error; the deprecated `--age` still works and is hidden from help (Task 3).

This slice is safely split into two PRs: **PR A = Tasks 1–4**, **PR B = Tasks 5–7** (both against `main`, both referencing #168).

---

### Task 1: Shared list-output seam + `sandbox list`

**Files:**
- Create: `cmd/agents-sandbox/list_output.go`
- Create: `cmd/agents-sandbox/list_output_test.go`
- Modify: `cmd/agents-sandbox/commands.go` (remove `printItems`; update `--quiet` help text)
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildListCmd`, lines ~154-222)
- Modify: `cmd/agents-sandbox/constants.go` (`pFlagNames` retained; no change unless a helper is added)
- Modify: `internal/termio/termio.go`, `internal/termio/printer.go`, `internal/termio/mock.go` (add `Print`)
- Modify: `cmd/agents-sandbox/cli_list_subcommand_test.go`
- Modify: `cmd/agents-sandbox/cli_help_test.go` (drop the `--names` expectation if present)

**Interfaces:**
- Produces:
  - `termio.UI.Print(msg string)` — writes a plain data line to stdout, **bypassing** quiet suppression; `Mock.PrintCalls []string` records each call.
  - `type listOutput int` with `listOutputTable`, `listOutputIdentifiers`, `listOutputJSON`.
  - `resolveListOutput(format string, quiet bool) (listOutput, error)`.
  - `printList[T any](ui termio.UI, mode listOutput, items []T, emptyMsg string, headers []string, jsonValue func([]T) any, cells ...func(T) string) error` — identifiers mode prints `cells[0](item)`.

- [ ] **Step 1: Write the failing `termio.Print` test**

In `internal/termio/printer_test.go` (mirror the existing quiet tests around line 275), add a test asserting that with `ui.SetQuiet(true)`, `ui.Print("row")` **still** writes `row` to stdout, while `ui.Out("row")` writes nothing.

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/termio/ -run TestPrintBypassesQuiet -v`
Expected: FAIL — `Print` undefined.

- [ ] **Step 3: Add `Print` to the UI seam**

- `termio.go`: add `Print(msg string)` to the `UI` interface (next to `Out`).
- `printer.go`: `func (p *printer) Print(msg string) { p.write(p.stdout, "", msg) }` — no `quiet` check.
- `mock.go`: add `PrintCalls []string`; `func (m *Mock) Print(msg string)` appends the stripped message under `m.mu` and writes it to `m.stdout` (match `Out`'s style).

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/termio/ -run TestPrintBypassesQuiet -v`
Expected: PASS.

- [ ] **Step 5: Write the failing `resolveListOutput` + `printList` unit tests**

`list_output_test.go`:
- `resolveListOutput("", false)` → `listOutputTable`; `("", true)` → `listOutputIdentifiers`; `(formatJSON, false)` → `listOutputJSON`.
- `(formatJSON, true)` → error containing "mutually exclusive".
- `("yaml", false)` → error containing "unsupported format".
- `printList` with `listOutputJSON` and an empty slice writes `[]` (not `null`); assert via a `termio.Mock` capturing `OutCalls`.

- [ ] **Step 6: Run them and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'TestResolveListOutput|TestPrintListJSONEmpty' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 7: Implement `list_output.go`**

```go
type listOutput int

const (
	listOutputTable listOutput = iota
	listOutputIdentifiers
	listOutputJSON
)

func resolveListOutput(format string, quiet bool) (listOutput, error)
func printList[T any](ui termio.UI, mode listOutput, items []T,
	emptyMsg string, headers []string, jsonValue func([]T) any, cells ...func(T) string) error
```

`printList`: identifiers → `ui.Print(cells[0](item))` per item; JSON → `json.MarshalIndent(jsonValue(items), "", "  ")` then `ui.Out`; table → `emptyMsg` via `ui.Info` when empty, else `ui.NewTable(headers...)` + `AddRow(cells...)` + `Print()`.

- [ ] **Step 8: Run the unit tests and confirm they pass**

Run: `go test ./cmd/agents-sandbox/ -run 'TestResolveListOutput|TestPrintListJSONEmpty' -v`
Expected: PASS.

- [ ] **Step 9: Wire `sandbox list` onto the seam; deprecate `--names`**

In `buildListCmd`:
- Compute identifiers mode as `quiet, _ := cmd.Flags().GetBool(pFlagQuiet)`, `namesOnly, _ := cmd.Flags().GetBool(pFlagNames)`, then `mode, err := resolveListOutput(format, quiet || namesOnly)`.
- Replace the `namesOnly`/`printSandboxesJSON`/`printItems` branches with a single `printList(ui, mode, sandboxes, "No sandboxes found.", sandboxListHeaders(), sandboxJSON, cells...)` call, where `sandboxJSON([]sandbox.Info) any` returns a non-nil `[]jsonSandbox`.
- Keep the `--names` flag registration and `pFlagNames`, then `_ = cmd.Flags().MarkDeprecated(pFlagNames, "use --quiet")` so it is hidden from help and warns when used.
- `commands.go`: delete the now-unused `printItems`; update the `--quiet` help to "Suppress stdout; on list commands print identifiers only".

- [ ] **Step 10: Update / add the CLI tests**

In `cli_list_subcommand_test.go`:
- Keep `TestListSandboxesQuietNames` (`--names`) — now asserts `ui.PrintCalls` and that `cmd.Flags().Lookup(pFlagNames).Deprecated != ""`.
- Add `TestListSandboxesQuiet` (`--quiet`) — same assertions via `--quiet`.
- Keep `TestListSandboxesFormatJSONAndNamesConflict`; add `TestListSandboxesFormatJSONQuietConflict`: `{cmdList, "--quiet", "--format", "json"}` → error containing "mutually exclusive".
- Keep `TestListSandboxesFormatJSON` (still asserts on `OutCalls`).

- [ ] **Step 11: Run the package tests and the linter**

Run: `go test ./cmd/agents-sandbox/ ./internal/termio/ && make fmt && make lint`
Expected: PASS, no lint findings.

- [ ] **Step 12: Commit**

```bash
git add cmd/agents-sandbox internal/termio
git commit -m "feat(cli): shared list output modes; sandbox list --quiet replaces --names (#168)"
```

---

### Task 2: `volume list` + `image list` output modes

**Files:**
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildImageCmd`, `buildVolumeCmd`)
- Modify: `cmd/agents-sandbox/cli_list_subcommand_test.go`

**Interfaces:**
- Consumes: `resolveListOutput`, `printList`, `ui.Print` (Task 1).

- [ ] **Step 1: Write failing tests**

Add `TestListImagesQuiet`, `TestListImagesFormatJSON`, `TestListVolumesQuiet`, `TestListVolumesFormatJSON`, and an empty-JSON case per list (asserting `[]`).
- Quiet: each reference/name appears in `ui.PrintCalls`; no header in `OutCalls`.
- JSON: unmarshal `strings.Join(ui.OutCalls, "\n")` into `[]struct{...}` and check the fields.

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'TestListImages(Quiet|FormatJSON)|TestListVolumes(Quiet|FormatJSON)' -v`
Expected: FAIL — flags/output not implemented.

- [ ] **Step 3: Implement**

For both list subcommands: add `--format` (`flagFormat`) and read the global quiet flag; call `resolveListOutput`; replace `printItems` with `printList`; add `imageJSON([]image.Info) any` and `volumeJSON([]volume.VolumeInfo) any` (non-nil slices) near `jsonSandbox`.

- [ ] **Step 4: Run and confirm pass**

Run: `go test ./cmd/agents-sandbox/ -run 'TestListImages|TestListVolumes' -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
make fmt && make lint
git add cmd/agents-sandbox
git commit -m "feat(cli): volume/image list --quiet and --format json (#168)"
```

---

### Task 3: `--older-than` canonical; deprecate `--age`

**Files:**
- Modify: `cmd/agents-sandbox/constants.go` (add `flagOlderThan = "older-than"`)
- Modify: `cmd/agents-sandbox/commands_system.go` (`setPruneFlags`, `resolvePruneAge`)
- Modify: `cmd/agents-sandbox/cli_fixture_test.go` (point `pruneAgeFlags` at `--older-than`, keep one `--age` case)
- Modify: prune command tests as needed

**Interfaces:**
- Produces: `--older-than` — canonical across every prune command, human durations via `viperconfig.ParseHumanDuration`, default `7d`. `--age`/`-a` retained but registered deprecated (hidden from help, warns when used, still honored when set).

- [ ] **Step 1: Write failing tests**

- `TestPruneOlderThan`: `--older-than 2w` behaves like the existing `--age 2w` case.
- `TestPruneOlderThanInvalid`: `--older-than nope` → error containing "invalid age".
- `TestPruneOlderThanAndAgeConflict`: both set → error "use either --older-than or --age, not both".
- `TestPruneAgeDeprecated`: `--age 2w` still prunes **and** `cmd.Flags().Lookup(flagAge).Deprecated != ""`.
- Repoint `pruneAgeFlags` in `cli_fixture_test.go` to `--older-than` variations; retain one `--age`/`-a` case to prove the deprecated flag still works.

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'TestPruneOlderThan|TestPruneAgeDeprecated|TestPruneOlderThanAndAge' -v`
Expected: FAIL — unknown flag.

- [ ] **Step 3: Implement**

- `setPruneFlags`: register `--older-than` as the primary flag (default `7d`); register `--age`/`-a` with default `""`, then `_ = cmd.Flags().MarkDeprecated(flagAge, "use --older-than")` (Cobra then hides it from help and prints a deprecation notice when it is used).
- `resolvePruneAge`: if both `--older-than` and `--age` are `Changed` → error "use either --older-than or --age, not both"; otherwise take whichever is changed; parse through the same `ParseHumanDuration` path (shared invalid-value error); otherwise fall through to `manualPruneAge`/the `7d` default.

- [ ] **Step 4: Run and confirm pass**

Run: `go test ./cmd/agents-sandbox/ -run 'TestPrune' -v`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
make fmt && make lint
git add cmd/agents-sandbox
git commit -m "feat(cli): --older-than alias for prune --age (#168)"
```

---

### Task 4: Prune reports as returned values + `--format json`

**Files:**
- Modify: `internal/sandbox/pruning/prune.go`, `images.go`, `volumes.go`, `sandboxes.go`, `helpers.go`, `cleanup.go`
- Create: `cmd/agents-sandbox/prune_output.go` (report → JSON DTOs)
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildPruneCmd` + the three group prune commands)
- Modify: `cmd/agents-sandbox/cli_prune_test.go`, `cli_volume_prune_test.go`, `cli_image_prune_test.go`, `cli_sandbox_prune_test.go`

**Interfaces:**
- `InvokePruneFunc` returns `(R, error)` (was `error`).
- `pruning.Prune(...)` returns `(*StaleReport, error)` (was `error`); it no longer prints.
- Group pruners no longer print a summary; they return their report. Renderers are exported: `pruning.RenderPruneSummary(ui, *StaleReport, dryRun)`, `RenderImageReport`, `RenderVolumeReport`, `RenderSandboxReport` (renamed from the private `print*Report`).
- Consumes: `resolveListOutput`-style `--format` parsing (reuse the `format`/`formatJSON` constants).

- [ ] **Step 1: Write failing tests**

- `TestPruneFormatJSON`: `setupCommandFixtures(t, "prune", "--format", "json")` with a stale VM/volume/image seeded; assert `strings.Join(ui.OutCalls, "\n")` unmarshals into an object with `sandboxes`, `volumes`, `images`, `docker_images`, and `details`.
- `TestVolumePruneFormatJSON`, `TestImagePruneFormatJSON`, `TestSandboxPruneFormatJSON`: same shape for each group prune.
- `TestPruneFormatUnknown`: `--format yaml` → error containing "unsupported format".

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'FormatJSON|FormatUnknown' -v`
Expected: FAIL — flag/behavior missing.

- [ ] **Step 3: Refactor `pruning` to return reports**

- `helpers.go`: `func InvokePruneFunc[R any](...) (R, error)` — return the lambda's report and error.
- `images.go` / `volumes.go` / `sandboxes.go`: delete the `print*Report(...)` call inside each pruner; rename the render funcs to exported `RenderImageReport` / `RenderVolumeReport` / `RenderSandboxReport` (bodies unchanged).
- `prune.go`: `func Prune(...) (*StaleReport, error)` — build the merged report and return it; no `printPruneSummary` call. Rename `printPruneSummary` → `RenderPruneSummary`.
- `cleanup.go`: `report, err := Prune(...)`; on success `RenderPruneSummary(ui, report, dryRun)` (via the redirect UI); warn on error as today.

- [ ] **Step 4: Add the CLI JSON output**

- `prune_output.go`: DTO structs with `json` tags and converters for each report type (`dry_run` from the flag), mapping `StaleEntry` with `type` as `entry.Type.String()`, plus `name`/`slug`/`digest`/`stale_for_seconds`.
- `buildPruneCmd` and the three group prunes: parse `--format` (reject anything but `json` with the shared error), and after the pruner returns: human → `Render*`; JSON → `ui.Out(string(marshalIndent(dto)))`.

- [ ] **Step 5: Update existing prune tests**

Existing summaries must now come from the CLI renderers (lines unchanged). Add `--format json` cases; confirm `slices.Contains(ui.OutCalls, "Pruned 1 home volume(s)")` etc. still hold. Update any test that called `pruning.Prune`/`InvokePruneFunc` directly for the new return values (`internal/sandbox/pruning/*_test.go`).

- [ ] **Step 6: Run and confirm pass**

Run: `go test ./internal/sandbox/pruning/ ./cmd/agents-sandbox/ -run 'Prune' -v && make compile-all`
Expected: PASS.

- [ ] **Step 7: Lint + commit**

```bash
make fmt && make lint
git add internal/sandbox/pruning cmd/agents-sandbox
git commit -m "feat(cli): prune report JSON and returned prune reports (#168)"
```

**(PR A ends here — open the PR against `main`, referencing #168.)**

---

### Task 5: `termio` confirmation helper

**Files:**
- Modify: `internal/termio/termio.go`, `internal/termio/printer.go`, `internal/termio/prompt.go`, `internal/termio/mock.go`
- Test: `internal/termio/prompt_test.go` (or nearest existing)

**Interfaces:**
- `termio.UI.Confirm(prompt string, defaultYes bool) (bool, error)`.
- Sentinel `termio.ErrConfirmationRequired`.
- `Mock`: `ConfirmResult bool`, `ConfirmErr error`, `ConfirmCalls []string`.

- [ ] **Step 1: Write the failing tests**

- With `SetAssumeYes(true)` → `Confirm("p", false)` returns `(true, nil)` and does not read stdin.
- Interactive (mock a TTY stdin via the existing test seam used by `Select`/`Input`) → returns the parsed answer.
- Non-interactive, not assume-yes → returns `(false, ErrConfirmationRequired)`.

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./internal/termio/ -run TestConfirm -v`
Expected: FAIL — `Confirm` undefined.

- [ ] **Step 3: Implement**

- `termio.go`: add `Confirm` to the interface; `var ErrConfirmationRequired = errors.New("confirmation required: pass --yes to proceed non-interactively")`.
- `prompt.go`: `Confirm` — assume-yes → true; not `IsInteractive()` → `(false, ErrConfirmationRequired)`; else read a line via `getStdinReader`, accept `y`/`yes` (case-insensitive), empty → `defaultYes`, else false.
- `mock.go`: add the three fields and a `Confirm` method (record prompt; return `ConfirmResult`/`ConfirmErr`).

- [ ] **Step 4: Run and confirm pass**

Run: `go test ./internal/termio/ -run TestConfirm -v && make lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/termio
git commit -m "feat(termio): confirm helper distinguishing --yes from --force (#168)"
```

---

### Task 6: Wire prune confirmation

**Files:**
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildPruneCmd` + the three group prunes)
- Modify: prune CLI tests (`cli_prune_test.go`, `cli_volume_prune_test.go`, `cli_image_prune_test.go`, `cli_sandbox_prune_test.go`)

**Interfaces:**
- Consumes: `termio.UI.Confirm` (Task 5).
- Produces: every prune command confirms before pruning.

- [ ] **Step 1: Write failing tests**

- `TestPruneNonInteractiveRequiresYes`: default fixture (non-interactive, not assume-yes) + `prune` → error `termio.ErrConfirmationRequired`, and the mock msb client received no removals.
- `TestPruneYesSkipsPrompt`: fixture with `ui.SetAssumeYes(true)` → prunes, no prompt recorded.
- Interactive-declined case (mock interactive + `ConfirmResult=false`) → prune returns a "cancelled" error and prunes nothing.

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./cmd/agents-sandbox/ -run 'TestPrune.*(RequiresYes|SkipsPrompt|Declined)' -v`
Expected: FAIL — prune currently prunes without confirming.

- [ ] **Step 3: Implement**

Add `confirmPrune(ui termio.UI) error`: `ok, err := ui.Confirm("Prune stale runtime objects?", false)`; if `err != nil` return it; if `!ok` return `errors.New("prune cancelled")`. Call it at the top of `buildPruneCmd` and each group prune `RunE`, **before** resolving the age/build state.

- [ ] **Step 4: Update existing prune tests**

Every existing prune invocation that expects success must now authorize: set `ui.SetAssumeYes(true)` in those tests (preferred, via a small helper or inline) or append `--yes` to the args. Keep the human/JSON assertions unchanged. Update `pruneAgeFlags`-driven subtests accordingly.

- [ ] **Step 5: Run and confirm pass**

Run: `go test ./cmd/agents-sandbox/ -run 'Prune' -v`
Expected: PASS.

- [ ] **Step 6: Lint + commit**

```bash
make fmt && make lint
git add cmd/agents-sandbox
git commit -m "feat(cli): confirm prune; --yes authorizes non-interactively (#168)"
```

---

### Task 7: Docs + CHANGELOG

**Files:**
- Modify: `docs/commands.md` (list `--quiet`/`--format json`, prune `--older-than`/`--format json`/`--yes`, the `--quiet` help meaning)
- Modify: `README.md` if it documents list/prune flags
- Modify: `CHANGELOG.md` (`[Unreleased]` → Added/Changed)

- [ ] **Step 1: Update docs**

Document: list commands support `--quiet` (identifiers only) and `--format json`; `sandbox list --names` is deprecated in favour of `--quiet`; prune accepts `--older-than` (canonical; `--age` is deprecated) and `--format json`, and requires `--yes` (or a prompt) to proceed.

- [ ] **Step 2: Add the CHANGELOG entries**

Under `[Unreleased]`: Added — list `--format json`/`--quiet`, prune `--format json`/`--older-than`; Changed — prune now confirms (use `--yes` non-interactively); Deprecated — `sandbox list --names` (use `--quiet`), prune `--age` (use `--older-than`).

- [ ] **Step 3: Verify docs**

Run: `make check`
Expected: fmt, lint, tests, and `ci/check-docs.sh` all pass.

- [ ] **Step 4: Commit**

```bash
git add docs README.md CHANGELOG.md
git commit -m "docs: shared CLI conventions for runtime management (#168)"
```

**(PR B ends here.)**

---

## Self-review notes

- **Spec coverage:** list output modes (T1, T2), shared `--yes` + confirmation (T5, T6), `--older-than` (T3), prune report JSON (T4), `--names` reconciliation (T1). Docs/CHANGELOG (T7).
- **Type consistency:** `listOutput`/`resolveListOutput`/`printList` defined once (T1) and consumed (T2); `Confirm`/`ErrConfirmationRequired` defined (T5) and consumed (T6); `InvokePruneFunc` and `Prune` return types changed together (T4).
- **Deferred to later slices:** no `volume/image/sandbox inspect`, no `image/sandbox remove`, no `df`/`storage` — those are #171, #172, #155.
- **Host-only caveat:** none of Slice 0 touches msb FFI, so all of it is unit/integration-testable in the VM. `make check` is the gate.
