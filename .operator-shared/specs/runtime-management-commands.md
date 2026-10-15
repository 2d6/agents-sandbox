---
description: Contract for extending agents-sandbox's CLI to cover microsandbox's *management* surface against its own private runtime home — volume inspect, image inspect/remove, sandbox remove/inspect/copy, a top-level `df` plus a `storage` group alias, and uniform `-q`/`--format json`/`--yes`/`--older-than` conventions — while deliberately not mirroring msb's full lifecycle, data-portability, registry, cloud, or self-install surface. Structured as ordered, independently shippable slices with shared groundwork landing first.
read_if: Adding or reviewing volume/image/sandbox inspection and removal commands, the `df`/`storage` accounting commands, prune confirmation/format flags, or command-surface consistency conventions.
---

# Runtime management commands

Date: 2026-10-15

Status: **design**. Not implemented. Tracked as issue #165. Builds on
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
- Bind `--older-than` as an alias of the existing `--age` on prune commands
  (both accept the existing human-duration syntax: `7d`, `2w`).
- JSON serialization for the prune reports (`prune` and each group `prune`).
- Reconcile `sandbox list --names` with the `-q/--quiet` spelling.

Depends on: nothing. Done when the existing `sandbox`/`volume`/`image` list and
prune commands use the shared helpers, and the flags are covered by tests.

### Slice 1 — Reference-guard seam (groundwork)

Generalize the fail-closed reference scan behind `volume remove`
(`state.HomeVolumeReferences`) into a reusable guard with an image analogue
(`state.ImageReferences`, or a small guard package). No user-facing command.

Depends on: nothing (parallel with Slice 0). Needed by Slice 4. Guard semantics:
an unreadable or corrupt project state file blocks the operation unless forced.

### Slice 2 — Home layout accounting seam (groundwork)

A function that enumerates the private home's storage categories with byte sizes
and host paths: runtime/FFI artifacts, runner images, home volumes, VM disks, the
buildkit cache, and docker images (when the docker backend is in use). No
user-facing command.

Depends on: nothing. Needed by Slice 5.

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

- `df [--verbose] [--format json]` — category totals from the Slice 2 seam;
  `--verbose` adds per-object rows and host paths; `humanize.FormatBytes` for
  display.
- `storage` group alias — `storage df` and `storage prune` alias the top-level
  `df` and `prune`; both also remain top-level.

Depends on: Slice 0, Slice 2.

### Slice 6 — Sandbox file transfer (optional)

- `sandbox copy`/`cp` — host↔VM file transfer over `SandboxFS`.
- *(Included because it was not in the drop list; this slice is the one that
  should be dropped if copy counts as the excluded "data portability" surface.)*

Depends on: Slice 0.

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
`doctor`/`update`/`downgrade`/`uninstall`, and a separate runtime-RAM `prune`
(agents-sandbox's extended `prune` already covers VMs, home volumes, docker
images, and msb images).

## Cross-cutting conventions (delivered by Slice 0)

- **Listing**: every list supports `-q/--quiet` and `--format json`; human tables
  remain the default.
- **Destructive**: every removal/prune supports `--yes` to authorize
  non-interactively, and removal supports `--force` to override a
  reference/ownership guard. `--force` overrides a guard; `--yes` skips a
  confirmation prompt — distinct, and they may coexist.
- **Age threshold**: `--age` gains `--older-than` (msb's spelling).
- **Prune report**: `prune` and each group `prune` support `--format json` and
  `-q`.
- **Tree**: new subcommands carry the `also: sandbox X` annotation and aliases
  consistent with the existing tree.
- **Guards are fail-closed**: an unreadable or corrupt project state file blocks
  a guarded removal unless `--force` is given.

## Left to the builder

- The reference scan for `image remove` (Slice 1): the image analogue of
  `state.HomeVolumeReferences`, and the exact tag/state source of truth.
- `df`'s category discovery (Slice 2): which private-home subtrees count as
  runtime vs image cache vs volumes vs VM disks, and how docker images fold in.
- Whether `sandbox copy` (Slice 6) belongs, and, if so, its argument grammar
  (`sandbox copy <name> <src> <dst>` vs a `cp`-style form).
- Whether to expose per-volume usage in `volume list` or only in `inspect`.
- Testing strategy per slice (table/JSON goldens, guard paths, fail-closed
  scans) and the CLI integration tests per the repo's conventions.
- Docs (`docs/`, `README.md`) and `CHANGELOG.md` updates, per slice.
