---
description: Contract for extending agents-sandbox's CLI to cover microsandbox's *management* surface against its own private runtime home — volume inspect, image inspect/remove, sandbox remove/inspect/copy, a top-level `df` plus a `storage` group alias, and uniform `-q`/`--format json`/`--yes`/`--older-than` conventions — while deliberately not mirroring msb's full lifecycle, data-portability, registry, cloud, or self-install surface. Structured as ordered, independently shippable slices with shared groundwork landing first.
read_if: Adding or reviewing volume/image/sandbox inspection and removal commands, the `df`/`storage` accounting commands, prune confirmation/format flags, or command-surface consistency conventions.
---

# Runtime management commands

Date: 2026-10-15

Status: **design; Slices 0–2 implemented** (Slice 0 merged in PR #174; Slice 1
on branch `issue-169-runtime-management-slice-1`, PR pending; Slice 2 on branch
`issue-170-runtime-management-slice-2`, PR pending). Tracked as issue #165;
delivered as sub-issues #168 (Slice 0, implemented), #169 (Slice 1,
implemented), #170 (Slice 2, implemented), #171 (Slice 3), #172 (Slice 4), and
#155 (Slice 5). Slice 6 is dropped. Builds on
`specs/runtime-home-decoupling.md` (the private `$MSB_HOME` home) and the
reference in `guides/microsandbox-release-notes.md`.

## Intent

agents-sandbox now owns its microsandbox runtime in a private home
(`$MSB_HOME`, `specs/runtime-home-decoupling.md`) and no longer installs or
advertises a user-facing `msb`. That removes the user's only way to inspect and
clean up the runtime's own objects (volumes, images, sandboxes) with msb's own
tooling. The `volume remove` command added during that migration is the first
piece of the replacement.

This spec defines the rest of the **management** surface agents-sandbox should
provide so a user can inspect and reclaim its runtime objects without an `msb`
binary: object inspection, object removal, storage accounting, and uniform prune
controls. It is a deliberately bounded slice of msb's CLI — not a front-end to
msb, and not a mirror of its lifecycle or portability features.

The work is specified as **ordered delivery slices** (below). Because every
slice depends on the same cross-cutting output/confirmation conventions and on a
couple of shared seams, **the groundwork slices (0–2) land before any
user-facing command**, and each remaining slice is independently shippable in
dependency order.

## Goals

- Provide read-only inspection of the objects agents-sandbox manages: home
  volumes, cached runner images, and project sandboxes/VMs.
- Provide explicit removal of volumes and images by name.
- Provide storage accounting over the private home (a `df` equivalent) and a
  `storage` command group.
- Make the listing/prune surface consistent: machine-readable output, quiet
  names-only output, non-interactive confirmation, and a uniform age flag.
- Keep all behavior scoped to the private runtime home and agents-sandbox's
  per-project/per-agent model.

## Non-goals

- Reproducing msb's full sandbox lifecycle (`create`/`start`/`restart`/`pause`/
  `resume`/`wait`), command execution (`exec`/`logs`/`metrics`), or arbitrary
  sandbox management. agents-sandbox's VMs are derived from a project + agent,
  not created ad hoc.
- Data-portability commands (`image pull`/`load`/`save`).
- Registry-credential management (`registry`/`registries`).
- Any cloud-backend surface (`context`) or msb self-management
  (`self`/`install`/`uninstall`/`downgrade`).
- Forks, snapshots, or SSH.

## Anti-goals

- A command surface that implies agents-sandbox is a general msb replacement,
  setting an expectation that user-arbitrary sandboxes or images are first-class.
- Removal commands that can silently orphan a project's recorded home volume or
  an in-use runner image.
- Destructive commands that prompt or block in non-interactive contexts.
- Shipping a user-facing command whose output/confirmation behavior is
  inconsistent with its siblings, because the shared conventions were not landed
  first.

## Constraints

- Private runtime home: every command operates on `$MSB_HOME` and the
  agents-sandbox state roots, never on a user's `~/.microsandbox`.
- Project-scoped identity: sandbox/volume/image identity is derived from project
  slug, agent, and the recorded state, not arbitrary user-chosen names, except
  where an object is addressed by its own name for a by-name operation
  (`volume remove`, `image remove`).
- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- Follow the existing command conventions: Cobra groups with aliases, the tree
  annotations (`also: sandbox X`), and `termio` for all output.

## Delivery slices

Slices are ordered; a slice depends only on earlier slices. Each slice is a
mergeable unit with its own tests, docs, and `CHANGELOG.md` entry.

### Slice 0 — Shared CLI conventions (groundwork)

No new user-facing command. Land the cross-cutting seam every later slice uses:

- A shared list-output path so a list can render, in addition to its default
  table, `--format json` (structured output on stdout) and `-q/--quiet`
  (identifiers only). Extend the existing `printItems` helper rather than
  duplicating per command.
- A shared `--yes` flag and non-interactive confirmation helper for destructive
  commands, distinct from `--force` (which overrides a guard).
- Make `--older-than` the canonical age flag on prune commands (human-duration
  syntax: `7d`, `2w`) and deprecate `--age`/`-a`: it stays functional when set
  but is hidden from help and warns when used.
- JSON serialization for the prune reports (`prune` and each group `prune`).
- Deprecate `sandbox list --names` as an alias of the `-q/--quiet` spelling
  (hidden, warns, still honored).

Depends on: nothing. Done when the existing `sandbox`/`volume`/`image` list and
prune commands use the shared helpers, and the flags are covered by tests.

### Slice 1 — Reference-guard seam (groundwork)

Generalize the fail-closed reference scan behind `volume remove`
(`state.HomeVolumeReferences`) into a reusable guard with an image analogue
(`state.ImageReferences`, or a small guard package). No user-facing command.

Depends on: nothing (parallel with Slice 0). Needed by Slice 4. Guard semantics:
an unreadable or corrupt project state file blocks the operation unless forced.

Resolved: an image is in use when a kept project state **or** a kept sandbox
references its reference — the union of the project-state scan (the
`HomeVolumeReferences` analogue) and the live kept-sandbox image refs already
computed for image pruning. Project state is the source of truth for ownership;
the live sandbox refs catch a digest-pinned VM whose tag is no longer the
per-agent `-latest`.

Implemented shape: the walk behind `state.HomeVolumeReferences` is generalized
into an unexported `state.projectStateReferences(extract)` with the same
fail-closed semantics (a missing state root is empty; an unreadable directory or
unreadable/corrupt state file is an error). `state.ImageReferences` reuses it,
mapping each state to the per-agent runner ref its slug and agent derive
(`naming.RunnerImageRef`, now the single builder behind `image.runnerTag`); a
legacy agent-less key owns none, since runner images are per-agent.
`image.InUseImageReferences(keptSandboxes)` unions that scan with the image refs of
the given kept sandboxes, attributing each to the key parsed from the sandbox
name and de-duplicating. It reads each sandbox's image from `Config` (not
`Image`, which swallows the config error) so an unreadable sandbox config fails
closed rather than silently dropping the reference.

### Slice 2 — Home layout accounting seam (groundwork)

A seam that enumerates the private home's storage categories with byte sizes and
host paths, feeding Slice 5's `df`. No user-facing command.

Prefer microsandbox's own storage observation (`StorageUsage`) for the objects it
owns — images, snapshots, sandboxes, volumes, and branch/snapshot memory — which
reports per-category and per-object usage and whose optional counters mean
*unknown* when nil (never zero). Walk the private home only for what microsandbox
does not report: the buildkit cache, docker images (when the docker backend is in
use), and runtime/FFI artifacts. `ErrUnsupportedOperation` from the observation
(cloud or older native libraries) degrades gracefully: the categories it covers
are marked unknown rather than reported as zero.

Depends on: nothing (parallel with Slices 0–1). Needed by Slice 5.

Implemented (issue #170) in `internal/sandbox/storage`: `Observe` emits the six
microsandbox categories (nil counters stay unknown; `ErrUnsupportedOperation`
marks them unknown with a note) followed by a `runtime` category that walks the
private home's `bin`/`lib` for the runtime/FFI artifacts. The host-side buildkit
cache and docker images were **deferred out of this seam**: the buildkit cache is
a named msb volume already covered by the `volumes` category, and host Docker's
store is outside the private home. Revisit when a docker-backend image store
lands.

### Slice 3 — Object inspection (read-only commands)

- `volume inspect <name>` — name, kind, host path (`Path()`), quota, used,
  capacity, disk format, disk fstype, labels, creation time (all already exposed
  by `msb.VolumeHandle`).
- `image inspect <ref>` — manifest/layers/config; reuse the existing
  `image.inspect` path plus `msb.Image.Inspect` (already used for digest
  resolution).
- `sandbox inspect <name>` — status, image, labels, mounts, resources, `ID`, and
  backend kind.
- Wire `volume list`/`image list` onto the Slice 0 JSON/quiet output.

Depends on: Slice 0.

### Slice 4 — Object removal

- `image remove <ref>...` (`rm`) — by-reference deletion with `-f/--force` and
  the Slice 1 image-reference guard (refuses an in-use runner image, naming the
  referencing projects).
- `sandbox remove <name>` (`rm`) — remove a project VM by name; `--force` to
  also drop persisted state.

Depends on: Slice 0 (confirmation/force conventions), Slice 1 (image guard).

### Slice 5 — Storage accounting

Tracked as issue **#155** (the canonical ticket for this slice).

- `df [--verbose] [--format json]` — category totals from the Slice 2 seam;
  `--verbose` adds per-object rows and host paths; `humanize.FormatBytes` for
  display.
- `storage` group alias — `storage df` and `storage prune` alias the top-level
  `df` and `prune`; both also remain top-level.

Depends on: Slice 0, Slice 2.

### Slice 6 — Sandbox file transfer (dropped)

`sandbox copy`/`cp` (host↔VM transfer over `SandboxFS`) is **dropped**: it is the
excluded data-portability surface, not part of the bounded management surface
this spec defines.

## Command surface contract

The full per-group contract, as the slices deliver it.

`volume`:

- `volume list` — table default; `-q/--quiet` (names only), `--format json`.
- `volume inspect <name>` — Slice 3 fields above.
- `volume remove <name>...` (`rm`) **exists** — by-name deletion with the
  fail-closed project-reference guard and `--force`. Unchanged.
- `volume prune` **exists** — unreferenced home volumes above `--age`. Gains the
  Slice 0 flags.
- `volume create` **excluded** — home volumes come from the runner image via
  `volume migrate`/`reset`/`edit`; a raw create would bypass that model.

`image`:

- `image list` — table default; `-q/--quiet` (references only), `--format json`.
- `image inspect <ref>` — Slice 3.
- `image remove <ref>...` (`rm`) — Slice 4, guarded.
- `image prune` **exists** (extended) — keeps per-agent `-latest` tags and
  in-use images. Gains `--yes`, `--format json`, and `-q`.
- `image pull`/`load`/`save` **excluded** — runner images are built, not moved.

`sandbox`:

- `sandbox list`, `sandbox run`, `sandbox shell`, `sandbox stop`,
  `sandbox kill` **exist**. Unchanged (list gains the Slice 0 flags).
- `sandbox remove <name>` (`rm`) — Slice 4.
- `sandbox inspect <name>` — Slice 3.
- `sandbox copy`/`cp` — Slice 6 (optional).
- `sandbox start`/`restart`/`exec`/`logs`/`metrics`/`pause`/`resume`/`wait`
  **excluded** — lifecycle/exec surface agents-sandbox does not expose.
- `sandbox modify`/`fork`/`snap`/`ssh`/`install` **excluded** — no fit for the
  project-VM model (reconfiguration flows through reprovision, not `msb modify`).

`storage` / `df`:

- `df` and the `storage` group — Slice 5.

Out of scope (explicit): `registry`/`registries`, `context`/`ctx`, `self`/
`doctor`/`update`/`downgrade`/`uninstall`, and `sandbox compact` (issue #157).
Runtime-RAM pruning is **not** excluded: it is tracked by issue #156
(microsandbox's `PruneStorage` over branch/snapshot memory backings) and enriches
the top-level `prune`. The existing extended `prune` covers VMs, home volumes,
docker images, and msb images, but not runtime RAM.

## Cross-cutting conventions (delivered by Slice 0)

- **Listing**: every list supports `-q/--quiet` and `--format json`; human tables
  remain the default.
- **Destructive**: every removal/prune supports `--yes` to authorize
  non-interactively, and removal supports `--force` to override a
  reference/ownership guard. `--force` overrides a guard; `--yes` skips a
  confirmation prompt — distinct, and they may coexist.
- **Age threshold**: `--older-than` is the canonical flag (msb's spelling);
  `--age` is deprecated (hidden, warns, still honored; both set together is an
  error).
- **Prune report**: `prune` and each group `prune` support `--format json` and
  `-q`. The report is rendered before an error is returned: a prune whose
  per-group errors are joined (top-level `prune` and `AutoPrune`) still reports
  what it removed first ("summary, then error"). Output mode and flag validation
  (`--format`/`--quiet` mutex) run before the confirmation prompt so bad flags
  fail fast rather than being masked by a non-interactive `ErrConfirmationRequired`.
- **Tree**: new subcommands carry the `also: sandbox X` annotation and aliases
  consistent with the existing tree.
- **Guards are fail-closed**: an unreadable or corrupt project state file blocks
  a guarded removal unless `--force` is given.

## Resolved decisions

- **Image reference scan (Slice 1)**: project state is the source of truth for
  ownership; the scan is the union of the `HomeVolumeReferences` analogue and the
  live kept-sandbox image refs. Fail-closed on unreadable or corrupt state.
- **`df` category discovery (Slice 2)**: microsandbox's `StorageUsage` supplies
  the categories it owns; the private-home walk supplies the buildkit cache,
  docker images (docker backend only), and runtime/FFI artifacts. Nil = unknown.
- **`sandbox copy` (Slice 6)**: dropped.
- **Per-volume usage**: exposed in `volume inspect` only; `volume list` stays
  cheap (no per-object stat).

## Left to the builder

- Testing strategy per slice (table/JSON goldens, guard paths, fail-closed
  scans) and the CLI integration tests per the repo's conventions.
- Docs (`docs/`, `README.md`) and `CHANGELOG.md` updates, per slice.
