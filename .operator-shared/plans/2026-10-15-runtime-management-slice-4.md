# Runtime Management Slice 4 (Object Removal) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `image remove <ref>...` and `sandbox remove <name>` to the CLI — explicit, guarded removal of the runner images and project VMs agents-sandbox manages — and make every removal command confirm under the shared `--yes` convention.

**Architecture:** Each removal is a package function that takes `context.Context`, its arguments, `force bool`, and `termio.UI`, and runs **guard → confirm → remove** so a guard refusal fails fast before any prompt. The image guard reuses the Slice 1 fail-closed seam (`image.InUseImageReferences`); the sandbox guard refuses a running VM unless forced and drops persisted state only when forced. Confirmation uses one new shared helper, `termio.ConfirmAction`.

**Tech Stack:** Go 1.26, Cobra, Viper, the `microsandbox` Go SDK v0.7.8 seam (`internal/sandbox/msb`), `termio`.

**Spec:** `.operator-shared/specs/runtime-management-commands.md` (Slice 4)

## Global Constraints

- Every command operates on the private runtime home (`$MSB_HOME`) and the agents-sandbox state roots only — never a user `~/.microsandbox`.
- `--force` overrides a guard; `--yes` skips a confirmation prompt — distinct flags that may coexist.
- Guards are fail-closed: an unreadable or corrupt project state file blocks a guarded removal unless `--force`.
- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- Follow existing conventions: Cobra group subcommands with `rm` aliases, `termio` for output, fail-closed errors that name the referencing projects.
- Do not restate implementation in the brain; keep the spec/CHANGELOG factual.

## Review Focus

- `image remove <missing-ref>` where the reference is not cached: msb returns a not-found error; the command must surface a clear error, never a panic or a silent success.
- `sandbox remove <name>` for a name that is not an agents-sandbox project VM (e.g. a user's own msb sandbox): must refuse rather than touching an object we do not own.
- A destructive removal in a non-interactive context (CI, scripts) without `--yes`: must fail fast with `termio.ErrConfirmationRequired`, never block on stdin.
- `image remove` when a project `state.yaml` is corrupt: must fail closed (guard error) instead of removing a possibly-in-use image.
- `sandbox remove --force` on an already-stopped VM: must still drop persisted state — `force` is the state-drop switch, not a running-only switch.

---

### Task 1: Shared confirmation helper and key formatter

**Files:**
- Modify: `internal/termio/termio.go` (add `ConfirmAction` near `ErrConfirmationRequired`)
- Modify: `internal/sandbox/state/references.go` (add `FormatKeys`)
- Test: `internal/termio/confirm_test.go` (create)
- Test: `internal/sandbox/state/references_test.go` (add `TestFormatKeys`)

**Interfaces:**
- Consumes: `termio.UI` (`Confirm(prompt string, defaultYes bool) (bool, error)`), `termio.ErrConfirmationRequired`.
- Produces:
  - `func ConfirmAction(ui UI, prompt string) error` — prompts; returns `ErrConfirmationRequired` non-interactively without `--yes`; returns `errors.New("cancelled")` when declined.
  - `func FormatKeys(keys []Key) string` — renders `Key` labels as sorted `slug/agent` (or `slug` for a legacy agent-less key) joined with `", "`.

- [ ] **Step 1: Write the failing tests**

In `internal/termio/confirm_test.go`:

```go
package termio

import (
	"errors"
	"testing"
)

func TestConfirmActionAssumesYes(t *testing.T) {
	ui := &Mock{}
	ui.SetAssumeYes(true)
	if err := ConfirmAction(ui, "Remove it?"); err != nil {
		t.Fatalf("ConfirmAction(assume-yes) = %v, want nil", err)
	}
	if len(ui.ConfirmCalls) != 0 {
		t.Errorf("assume-yes must not prompt, got %v", ui.ConfirmCalls)
	}
}

func TestConfirmActionNonInteractiveFails(t *testing.T) {
	ui := &Mock{}
	err := ConfirmAction(ui, "Remove it?")
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("ConfirmAction(non-interactive) = %v, want ErrConfirmationRequired", err)
	}
}

func TestConfirmActionDeclined(t *testing.T) {
	ui := &Mock{IsInteractiveResult: true, ConfirmResult: false}
	if err := ConfirmAction(ui, "Remove it?"); err == nil ||
		err.Error() != "cancelled" {
		t.Fatalf("ConfirmAction(declined) = %v, want cancelled", err)
	}
}

func TestConfirmActionAccepted(t *testing.T) {
	ui := &Mock{IsInteractiveResult: true, ConfirmResult: true}
	if err := ConfirmAction(ui, "Remove it?"); err != nil {
		t.Fatalf("ConfirmAction(accepted) = %v, want nil", err)
	}
}
```

In `internal/sandbox/state/references_test.go`, add:

```go
func TestFormatKeys(t *testing.T) {
	got := FormatKeys([]Key{
		{Slug: "legacy-aBc1234D"},
		{Slug: "proj-aBc1234D", Agent: "opencode"},
	})
	if got != "legacy-aBc1234D, proj-aBc1234D/opencode" {
		t.Fatalf("FormatKeys = %q, want %q", got, "legacy-aBc1234D, proj-aBc1234D/opencode")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/termio/ -run TestConfirmAction -v && go test ./internal/sandbox/state/ -run TestFormatKeys -v`
Expected: FAIL — `undefined: ConfirmAction` / `undefined: FormatKeys`.

- [ ] **Step 3: Implement `ConfirmAction` in `internal/termio/termio.go`**

Add `errors` to the imports if absent.

```go
// ConfirmAction prompts for a destructive action and maps the answer to an
// error: nil on yes, ErrConfirmationRequired when non-interactive without
// assume-yes (from ui.Confirm), and a "cancelled" error when declined.
func ConfirmAction(ui UI, prompt string) error {
	ok, err := ui.Confirm(prompt, false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("cancelled")
	}
	return nil
}
```

- [ ] **Step 4: Implement `FormatKeys` in `internal/sandbox/state/references.go`**

Add `sort` to the imports; `strings` is already imported.

```go
// FormatKeys renders referencing project keys as "slug/agent" (or "slug" for a
// legacy agent-less key), sorted for stable output.
func FormatKeys(keys []Key) string {
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/termio/ ./internal/sandbox/state/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/termio/termio.go internal/termio/confirm_test.go internal/sandbox/state/references.go internal/sandbox/state/references_test.go
git commit -m "feat(termio,state): shared removal confirmation and key formatter (#172)"
```

---

### Task 2: Retrofit `volume remove` to confirm

**Files:**
- Modify: `internal/sandbox/volume/remove.go` (signature, guard→confirm→remove, drop local `formatKeys`)
- Modify: `cmd/agents-sandbox/commands_system.go` (`buildVolumeRemoveCmd(ui)` at `:667-686`, caller at `:664`)
- Test: `internal/sandbox/volume/remove_test.go`
- Test: `cmd/agents-sandbox/cli_volume_remove_test.go`

**Interfaces:**
- Consumes: `termio.ConfirmAction` (Task 1), `state.FormatKeys` (Task 1).
- Produces: `func RemoveVolumes(ctx context.Context, names []string, force bool, ui termio.UI) error` — guard first (unless `force`), then confirm, then remove each by name. Removes the old 3-arg signature.

- [ ] **Step 1: Update `internal/sandbox/volume/remove.go`**

Change the signature and add confirmation **after** the guard, and delete the local `formatKeys` (now `state.FormatKeys`).

```go
func RemoveVolumes(ctx context.Context, names []string, force bool, ui termio.UI) error {
	if !force {
		references, err := state.HomeVolumeReferences()
		if err != nil {
			return fmt.Errorf("check volume references: %w", err)
		}
		if err := refuseReferencedVolumes(names, references); err != nil {
			return err
		}
	}
	if err := termio.ConfirmAction(ui, fmt.Sprintf("Remove %d home volume(s)?", len(names))); err != nil {
		return err
	}
	for _, name := range names {
		if err := msb.Get().RemoveVolume(ctx, name); err != nil {
			return fmt.Errorf("remove volume %q: %w", name, err)
		}
	}
	return nil
}
```

In `refuseReferencedVolumes`, replace `formatKeys(keys)` with `state.FormatKeys(keys)`. Delete the `formatKeys` func and the `sort`/`strings` imports if now unused (`strings` is still used by `strings.Join`; drop `sort`).

- [ ] **Step 2: Update the CLI in `cmd/agents-sandbox/commands_system.go`**

Change `buildVolumeRemoveCmd()` to take `ui termio.UI`, pass it through, and update the call site in `buildVolumeCmd`:

```go
cmd.AddCommand(buildVolumeRemoveCmd(ui))
```

```go
func buildVolumeRemoveCmd(ui termio.UI) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     cmdRemove + " <name>...",
		Aliases: cmdRemoveAliases,
		Args:    cobra.MinimumNArgs(1),
		Short:   "Remove home volumes by name",
		Long: "Remove the named home volumes. Refuses when a volume is still referenced by a " +
			"project's state; pass --force to remove it anyway. Prompts unless --yes is set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return volume.RemoveVolumes(cmd.Context(), args, force, ui)
		},
	}
	cmd.Flags().BoolVar(&force, flagForce, false, "Remove even when a project still references the volume")
	return cmd
}
```

- [ ] **Step 3: Update `internal/sandbox/volume/remove_test.go`**

Add `"github.com/inoio/agents-sandbox/internal/termio"`. Pass a UI to every `RemoveVolumes` call:
- Guard/corrupt-scan tests: use `&termio.Mock{}` (confirmation is never reached).
- `TestRemoveVolumesForceBypassesReference`, `TestRemoveVolumesRemovesSeveral`: use an assume-yes UI:

```go
ui := &termio.Mock{}
ui.SetAssumeYes(true)
```

Delete `TestFormatKeys` (moved to `state` in Task 1).

- [ ] **Step 4: Update `cmd/agents-sandbox/cli_volume_remove_test.go`**

Add `--yes` to the success paths (`removes an unreferenced volume`, `rm alias removes`, `force overrides the reference`) so the confirmation is authorized. Add two cases:

```go
t.Run("fails fast without --yes in non-interactive mode", func(t *testing.T) {
	cmd, _ := setupCommandFixtures(t, "volume", "remove", removeTestVolume)
	m := &msb.MockMsbClient{}
	msb.WithMsbMock(t, m)
	err := cmd.Execute()
	if !errors.Is(err, termio.ErrConfirmationRequired) {
		t.Fatalf("expected ErrConfirmationRequired, got: %v", err)
	}
	if len(m.RemovedVolumes) != 0 {
		t.Errorf("RemovedVolumes = %v, want none", m.RemovedVolumes)
	}
})

t.Run("refuses a referenced volume before prompting", func(t *testing.T) {
	cmd, ui := setupCommandFixtures(t, "volume", "remove", removeTestVolume)
	m := &msb.MockMsbClient{}
	msb.WithMsbMock(t, m)
	if err := state.WriteState(
		state.Key{Slug: "proj-aBc1234D", Agent: "opencode"},
		state.HomeState{HomeVolume: removeTestVolume},
	); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected refusal, got %v", err)
	}
	if len(ui.ConfirmCalls) != 0 {
		t.Errorf("guard must run before the prompt, got %v", ui.ConfirmCalls)
	}
})
```

Add `"errors"` and `"github.com/inoio/agents-sandbox/internal/termio"` to the imports.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/sandbox/volume/ ./cmd/agents-sandbox/ -run 'VolumeRemove|RemoveVolumes' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/sandbox/volume/remove.go internal/sandbox/volume/remove_test.go cmd/agents-sandbox/commands_system.go cmd/agents-sandbox/cli_volume_remove_test.go
git commit -m "feat(volume): confirm volume remove under the shared --yes convention (#172)"
```

---

### Task 3: `image remove <ref>...`

**Files:**
- Create: `internal/sandbox/image/remove.go`
- Modify: `cmd/agents-sandbox/commands_system.go` (add `buildImageRemoveCmd(ui)`; wire into `buildImageCmd` at `:565-568`)
- Test: `internal/sandbox/image/remove_test.go` (create)
- Test: `cmd/agents-sandbox/cli_image_remove_test.go` (create)

**Interfaces:**
- Consumes: `image.InUseImageReferences(keptSandboxes []msb.SandboxHandle) (map[string][]state.Key, error)` (Slice 1, `references.go:23`); `msb.Get().ListSandboxes(ctx, nil)`; `msb.Get().ImageRemove(ctx, ref, force)`; `termio.ConfirmAction`; `state.FormatKeys`; `naming.VMPrefix`, `naming.TaskPrefix`.
- Produces: `func RemoveImages(ctx context.Context, refs []string, force bool, ui termio.UI) error`.

- [ ] **Step 1: Write the failing CLI test** in `cmd/agents-sandbox/cli_image_remove_test.go`

Use `setupCommandFixtures`, `msb.WithMsbMock`, and assert on `m.RemovedImages` (`[]msb.MockRemoveImageCall{Ref, Force}`). Cover: guard refusal naming the referencing project (seed a `state.WriteState` for `proj-aBc1234D/opencode` and request `naming.RunnerImageRef("proj-aBc1234D", "opencode")`); `--force --yes` removes; `--yes` removes an unreferenced image; non-interactive without `--yes` → `termio.ErrConfirmationRequired`; `rm` alias; missing ref surfaces the msb error.

```go
func TestImageRemoveRefusesReferenced(t *testing.T) {
	ref := naming.RunnerImageRef("proj-aBc1234D", "opencode")
	if err := state.WriteState(
		state.Key{Slug: "proj-aBc1234D", Agent: "opencode"},
		state.HomeState{},
	); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	cmd, _ := setupCommandFixtures(t, "image", "remove", "--yes", ref)
	m := &msb.MockMsbClient{}
	msb.WithMsbMock(t, m)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "proj-aBc1234D/opencode") {
		t.Fatalf("expected refusal naming the project, got %v", err)
	}
	if len(m.RemovedImages) != 0 {
		t.Errorf("RemovedImages = %v, want none", m.RemovedImages)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/agents-sandbox/ -run TestImageRemove -v`
Expected: FAIL — the `image remove` subcommand does not exist.

- [ ] **Step 3: Implement `internal/sandbox/image/remove.go`**

```go
package image

import (
	"context"
	"fmt"
	"strings"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// RemoveImages removes the named runner image references. Unless force is set,
// it refuses when a reference is still in use — recorded by a project state or
// held by a live project/task sandbox — naming the referencing projects, so an
// in-use image is never silently deleted. The scan fails closed.
func RemoveImages(ctx context.Context, refs []string, force bool, ui termio.UI) error {
	if !force {
		references, err := inUseImageReferences(ctx)
		if err != nil {
			return fmt.Errorf("check image references: %w", err)
		}
		if err := refuseReferencedImages(refs, references); err != nil {
			return err
		}
	}
	if err := termio.ConfirmAction(ui, fmt.Sprintf("Remove %d runner image(s)?", len(refs))); err != nil {
		return err
	}
	for _, ref := range refs {
		if err := msb.Get().ImageRemove(ctx, ref, force); err != nil {
			return fmt.Errorf("remove image %q: %w", ref, err)
		}
	}
	return nil
}

// inUseImageReferences unions the project-state ownership scan with the image
// references held by the live launcher sandboxes (project VMs and task
// workers), reusing the fail-closed Slice 1 seam.
func inUseImageReferences(ctx context.Context) (map[string][]state.Key, error) {
	handles, err := msb.Get().ListSandboxes(ctx, nil)
	if err != nil {
		return nil, err
	}
	kept := make([]msb.SandboxHandle, 0, len(handles))
	for _, h := range handles {
		name := h.Name()
		if strings.HasPrefix(name, naming.VMPrefix) || strings.HasPrefix(name, naming.TaskPrefix) {
			kept = append(kept, h)
		}
	}
	return InUseImageReferences(kept)
}

// refuseReferencedImages returns an error naming every requested reference that
// is still in use, so the caller can force the removal explicitly.
func refuseReferencedImages(refs []string, references map[string][]state.Key) error {
	var refused []string
	for _, ref := range refs {
		if keys := references[ref]; len(keys) > 0 {
			refused = append(refused, fmt.Sprintf("%s (referenced by %s)", ref, state.FormatKeys(keys)))
		}
	}
	if len(refused) == 0 {
		return nil
	}
	return fmt.Errorf(
		"refusing to remove image(s) still in use: %s; re-run with --force to remove anyway",
		strings.Join(refused, "; "),
	)
}
```

- [ ] **Step 4: Wire the CLI command in `cmd/agents-sandbox/commands_system.go`**

Add `cmd.AddCommand(buildImageRemoveCmd(ui))` in `buildImageCmd` (after `buildImageInspectCmd`), and:

```go
// buildImageRemoveCmd builds `image remove <ref>...`, which deletes cached
// runner images by reference. It refuses when an image is still in use unless
// --force is set, and prompts unless --yes is set.
func buildImageRemoveCmd(ui termio.UI) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     cmdRemove + " <ref>...",
		Aliases: cmdRemoveAliases,
		Args:    cobra.MinimumNArgs(1),
		Short:   "Remove cached runner images by reference",
		Long: "Remove the named cached runner images. Refuses when an image is still in use by a " +
			"project's state or a live sandbox; pass --force to remove it anyway. Prompts unless --yes is set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return image.RemoveImages(cmd.Context(), args, force, ui)
		},
	}
	cmd.Flags().BoolVar(&force, flagForce, false, "Remove even when a project or sandbox still references the image")
	return cmd
}
```

- [ ] **Step 5: Add package unit tests** in `internal/sandbox/image/remove_test.go`

Model on `references_test.go`: `configpaths.WithMockConfigPaths(t)`; seed state; a `msb.MockMsbClient` with `Sandboxes` handles carrying an `Image_`. Cover: `refuseReferencedImages` names projects and dedupes; `RemoveImages` force path skips the guard and calls `ImageRemove` with `force=true`; fail-closed on corrupt state.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/sandbox/image/ ./cmd/agents-sandbox/ -run 'ImageRemove|RemoveImages|RefuseReferencedImages' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/sandbox/image/remove.go internal/sandbox/image/remove_test.go cmd/agents-sandbox/commands_system.go cmd/agents-sandbox/cli_image_remove_test.go
git commit -m "feat(image): guard and confirm image remove (#172)"
```

---

### Task 4: `sandbox remove <name>`

**Files:**
- Create: `internal/sandbox/vm/remove.go`
- Modify: `cmd/agents-sandbox/commands_system.go` (add `buildSandboxRemoveCmd(ui)`; wire into `buildSandboxCmd` at `:689-703`)
- Test: `internal/sandbox/vm/remove_test.go` (create)
- Test: `cmd/agents-sandbox/cli_sandbox_remove_test.go` (create)

**Interfaces:**
- Consumes: `msb.Get().GetSandbox(ctx, name)`; `msb.IsSandboxInactive(status)`; `msb.IsReplaced(err)`, `msb.IsNotFound(err)`; `naming.VMPrefix`, `naming.ArtifactFor(name)`; `state.RemoveState(key)`; `termio.ConfirmAction`.
- Produces: `func RemoveSandbox(ctx context.Context, name string, force bool, ui termio.UI) error`.

- [ ] **Step 1: Write the failing CLI test** in `cmd/agents-sandbox/cli_sandbox_remove_test.go`

Cover: removing a stopped VM calls `Remove`; a running VM without `--force` is refused and not removed; `--force` on a running VM stops then removes; `--force` drops the persisted state (`state.ReadState` returns not-found afterwards); a non-project name is refused; non-interactive without `--yes` → `termio.ErrConfirmationRequired`; `rm` alias.

```go
func TestSandboxRemoveRefusesRunningWithoutForce(t *testing.T) {
	handle := &msb.MockSandboxHandle{Name_: "agents-sandbox-vm-aBc1234D", Status_: msbSdk.SandboxStatusRunning}
	m := &msb.MockMsbClient{}
	m.SetGotSandbox(handle)
	cmd, _ := setupCommandFixtures(t, "sandbox", "remove", "--yes", handle.Name_)
	msb.WithMsbMock(t, m)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected a running refusal, got %v", err)
	}
	if handle.DidRmv {
		t.Error("running sandbox must not be removed without --force")
	}
}
```

Add `msbSdk "github.com/superradcompany/microsandbox/sdk/go"` to the imports.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/agents-sandbox/ -run TestSandboxRemove -v`
Expected: FAIL — the `sandbox remove` subcommand does not exist.

- [ ] **Step 3: Implement `internal/sandbox/vm/remove.go`**

```go
package vm

import (
	"context"
	"fmt"
	"strings"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// RemoveSandbox removes a project VM by name. It removes only agents-sandbox
// project VMs, refuses a running VM unless force is set (a forced removal stops
// it first), and drops the persisted project state only when force is set.
func RemoveSandbox(ctx context.Context, name string, force bool, ui termio.UI) error {
	if !strings.HasPrefix(name, naming.VMPrefix) {
		return fmt.Errorf("%q is not an agents-sandbox project VM", name)
	}
	handle, err := msb.Get().GetSandbox(ctx, name)
	if err != nil {
		return fmt.Errorf("remove sandbox %q: %w", name, err)
	}
	if !msb.IsSandboxInactive(handle.Status()) {
		if !force {
			return fmt.Errorf("sandbox %q is running; stop it first or pass --force", name)
		}
		if err := handle.Stop(ctx); err != nil {
			return fmt.Errorf("stop sandbox %q: %w", name, err)
		}
	}
	if err := termio.ConfirmAction(ui, fmt.Sprintf("Remove sandbox %q?", name)); err != nil {
		return err
	}
	if err := handle.Remove(ctx); err != nil {
		if msb.IsReplaced(err) {
			return fmt.Errorf("remove sandbox %q: %w", name, err)
		}
		if msb.IsNotFound(err) {
			return dropSandboxState(name, force)
		}
		return fmt.Errorf("remove sandbox %q: %w", name, err)
	}
	return dropSandboxState(name, force)
}

// dropSandboxState removes the persisted per-agent state that points at a
// project VM, but only when force is set. A legacy agent-less or unparsable
// name drops nothing.
func dropSandboxState(name string, force bool) error {
	if !force {
		return nil
	}
	artifact := naming.ArtifactFor(name)
	if artifact.Slug == "" || artifact.Agent == "" {
		return nil
	}
	if err := state.RemoveState(state.Key{Slug: artifact.Slug, Agent: artifact.Agent}); err != nil {
		return fmt.Errorf("remove state for sandbox %q: %w", name, err)
	}
	return nil
}
```

- [ ] **Step 4: Wire the CLI command in `cmd/agents-sandbox/commands_system.go`**

Add `cmd.AddCommand(buildSandboxRemoveCmd(ui))` in `buildSandboxCmd`, and:

```go
// buildSandboxRemoveCmd builds `sandbox remove <name>`, which removes a project
// VM by name. It refuses a running VM unless --force is set (which stops it
// first), and drops the persisted project state only when --force is set.
func buildSandboxRemoveCmd(ui termio.UI) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     cmdRemove + " <name>",
		Aliases: cmdRemoveAliases,
		Args:    cobra.ExactArgs(1),
		Short:   "Remove a project sandbox VM by name",
		Long: "Remove an agents-sandbox project VM. Refuses a running VM unless --force is set, " +
			"which stops it first. --force also drops the project's persisted state. Prompts unless --yes is set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.RemoveSandbox(cmd.Context(), args[0], force, ui)
		},
	}
	cmd.Flags().BoolVar(&force, flagForce, false, "Remove a running VM and drop its persisted project state")
	return cmd
}
```

- [ ] **Step 5: Add package unit tests** in `internal/sandbox/vm/remove_test.go`

Model on the CLI cases at the package level: prefix refusal, running refusal, force stop+remove, force state drop (`state.ReadState` returns not-found), replace fails closed (`RemoveErr` seeded with a replaced error, e.g. via `msb.IsReplaced`), and the assume-yes UI. Note: `remove.go`'s package is `vm`, whose tests already set up `configpaths.WithMockConfigPaths`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/sandbox/vm/ ./cmd/agents-sandbox/ -run 'SandboxRemove|RemoveSandbox' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/sandbox/vm/remove.go internal/sandbox/vm/remove_test.go cmd/agents-sandbox/commands_system.go cmd/agents-sandbox/cli_sandbox_remove_test.go
git commit -m "feat(sandbox): guarded sandbox remove with running/state semantics (#172)"
```

---

### Task 5: Docs, CHANGELOG, and brain sync

**Files:**
- Modify: `docs/commands.md` (add `#### image remove` under `### image`; add `#### sandbox remove` under `### sandbox`; update `#### agents-sandbox volume remove <name>...` at `:576`)
- Modify: `CHANGELOG.md` (`## [Unreleased]`)
- Modify: `.operator-shared/specs/runtime-management-commands.md` (status line; Slice 4 implemented shape; cross-cutting confirmation note)
- Modify: `.operator-shared/catalog.md` (add this plan entry)

**Interfaces:**
- Consumes: the finished command behavior from Tasks 2–4.
- Produces: user-facing docs and the durable brain record.

- [ ] **Step 1: Update `docs/commands.md`**

- `image remove <ref>...` (`rm`): by-reference deletion; `--force` overrides the in-use guard; prompts unless `--yes`. Table the flags `--force` and the global `--yes`.
- `sandbox remove <name>` (`rm`): removes a project VM; refuses a running VM unless `--force` (stops it first); `--force` drops persisted state; prompts unless `--yes`.
- `volume remove <name>...`: add that it now prompts unless `--yes` (keeping the `--force` guard note).

- [ ] **Step 2: Update `CHANGELOG.md`**

Add under `## [Unreleased]`:

```markdown
- CLI: `image remove <ref>...` (`rm`) deletes cached runner images by reference and `sandbox remove <name>` (`rm`)
  removes a project VM. Both refuse an in-use/guarded object unless `--force` (naming the referencing projects for
  images), refuse a running VM unless `--force`, and prompt unless `--yes` is set.
- Behavior: `volume remove` now prompts for confirmation; pass `--yes` to skip it non-interactively.
```

- [ ] **Step 3: Update the spec**

- Status line: record Slices 0–3 merged and Slice 4 implemented (branch/pending PR).
- Slice 4 section: record the resolved decisions — every removal (including `volume remove`) now confirms under `--yes`; `sandbox remove` refuses a running VM unless `--force`, which stops it and drops persisted state; the image guard unions the project-state scan with live project/task sandbox image refs.
- Cross-cutting "Destructive" bullet: note that all removal commands prompt and the retrofit of `volume remove`.

- [ ] **Step 4: Update `.operator-shared/catalog.md`**

Add a `plans/` entry for `2026-10-15-runtime-management-slice-4.md` with a Description and a Read If.

- [ ] **Step 5: Verify docs and commit**

Run: `make check` (fmt, lint, test, docs linkcheck) and fix anything it reports.
Expected: PASS.

```bash
git add docs/commands.md CHANGELOG.md .operator-shared/specs/runtime-management-commands.md .operator-shared/catalog.md
git commit -m "docs: record runtime-management Slice 4 (#172)"
```

---

## Self-Review

- **Spec coverage:** Slice 4 requires `image remove` (guarded, naming projects) — Task 3; `sandbox remove` with `--force` dropping state — Task 4; confirmation under `--yes` for all removals — Tasks 1–2 (retrofit) and 3–4 (new commands); fail-closed image guard — Task 3 reuses Slice 1. Docs/CHANGELOG — Task 5. Covered.
- **Step scan:** Each step names the exact signature, guard order, and test assertions; bodies appear only where the algorithm (guard→confirm→remove ordering, name-key derivation) is not implied by a signature.
- **Type consistency:** `RemoveVolumes`/`RemoveImages`/`RemoveSandbox` all take `(ctx, ..., force bool, ui termio.UI)`; helpers `inUseImageReferences`, `refuseReferencedImages`, `dropSandboxState` are defined in the task that uses them; `state.FormatKeys` and `termio.ConfirmAction` are produced in Task 1 before any consumer.
- **Review Focus:** each of the five lines has an owning task test (missing-ref error in Task 3 Step 1; non-VM-name refusal in Task 4 Step 1; non-interactive `ErrConfirmationRequired` in Tasks 2 and 3; corrupt-state fail-closed in Task 2 Step 5 and Task 3 Step 5; forced-stopped state drop in Task 4 Step 1).
- **Proportion:** five focused tasks; no task transcribes an implementation the interfaces and tests already determine.
