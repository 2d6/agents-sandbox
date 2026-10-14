---
description: Design for decoupling agents-sandbox's microsandbox runtime home from any user-installed microsandbox: a private XDG data home, no user-facing msb, retired version-matching recovery, and an opt-in copy of legacy home volumes recorded as `runtime_home_migration.dismissed` in root `state.yaml`.
read_if: Implementing or reviewing runtime-home decoupling, the private `MSB_HOME`, removal of the runtime mismatch recovery menu, or the legacy home-volume copy prompt.
---

# Runtime home decoupling

Date: 2026-10-13

Status: **implemented** — all Decisions below are in place. The implementation
plan is [2026-10-13-runtime-home-decoupling](../plans/2026-10-13-runtime-home-decoupling.md).
The single item still outstanding is the ownership-mapping verification under
"Open verification".

## Intent

agents-sandbox manages a microsandbox (`msb`) runtime today by sharing the
global `~/.microsandbox` home with whatever the user may have installed there.
That coupling is the source of the version-matching machinery: because both
sides share one runtime database, one binary, and one set of artifacts, a
user's `msb` and the SDK linked into agents-sandbox must agree, and the tool
resorts to upgrading, downgrading, or "brave mode" when they don't.

This design fully decouples the two. agents-sandbox keeps its runtime in a
private home of its own; a user's own microsandbox install lives side by side in
`~/.microsandbox`, and neither can see or disturb the other. The user may run
any `msb` version they like without affecting agents-sandbox, and vice versa.

## Goals

- agents-sandbox provisions and uses the exact runtime its linked SDK requires,
  in a home it owns, with no dependency on any user-installed runtime.
- The user's `~/.microsandbox` is never read or written by normal operation.
- No user-facing `msb` command is installed or advertised by agents-sandbox.
- The version-mismatch recovery menu is retired, because the mismatch it
  handled can no longer arise from the user's side.
- Existing users get a one-time, opt-in path to carry their old sandbox home
  volumes into the new private home, without forcing it.

## Non-goals

- Installing, exposing, or managing an `msb` for the user to run.
- Reading or writing the user's `~/.microsandbox` outside the opt-in copy
  (which reads only, and only our own name-prefixed volumes).
- A shared runtime database or any artifact crossing between the two homes.
- Matching versions between our runtime and the user's.

## Anti-goals

- Something that works, but couples the two homes such that a user's `msb`
  version change can break agents-sandbox (or the reverse).
- A user-visible `msb` binary that agents-sandbox owns and the user may come to
  depend on.
- Any prompt or behavior that nags, blocks, or loses the user's data silently.

## Constraints

- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- The SDK's FFI extraction root and the native runtime resolver both key off
  `$MSB_HOME`; this is the single lever for relocating the home.

## Current state (grounding)

- `internal/sandbox/runtime/runtime.go` inspects the selected runtime before any
  SDK call and drives a recovery menu (`recoverRuntimeMismatch`) offering
  launcher upgrade, `msb` upgrade/downgrade, brave mode, issue, or quit.
- `selectRuntime` treats `MSB_HOME` as the managed home and only marks a runtime
  "external" via `MSB_PATH` / `MSB_LIBKRUNFW_PATH` / persisted paths.
- `internal/sandbox/msb/msb.go` calls `EnsureRuntime` / `InstallRuntime` with an
  empty `RuntimeConfig{}`, so the SDK falls back to its default home.
- The SDK's `installDir()` (`setup.go`) and the native resolver both read
  `$MSB_HOME`, defaulting to `~/.microsandbox`.
- `internal/sandbox/doctor/install.go` prints PATH hints so the user can invoke
  the installed `msb`.
- A home volume is a `VolumeKindDir` — a plain host directory at
  `$MSB_HOME/volumes/<name>`; its host-facing path is exposed by `Volume.Path()`.
- Home volumes are named `agents-sandbox-home-<slug>-<agent>-<timestamp>`
  (`internal/sandbox/volume`), and the project → volume mapping lives in
  `~/.local/state/agents-sandbox/<slug>/<agent>/state.yaml`, independent of the
  msb home.
- Launcher-global state currently sprawls across feature-named files at the
  state root (`upgrade.json`, `updater.yaml`); making root `state.yaml` the one
  launcher-global document is a separate concern, tracked in issue #141.

## Decisions

### Private runtime home

- Location: the tool's data dir, `$XDG_DATA_HOME/agents-sandbox/msb`
  (`~/.local/share/agents-sandbox/msb`). Volumes are persistent user data, not
  cache, so the data dir is the right scope (not the cache dir; the state dir is
  semantically for state).
- agents-sandbox sets `MSB_HOME` to this directory at CLI startup, before any
  SDK call, so the FFI cache, runtime database, VM records, volumes, and images
  all live there. The SDK-pinned runtime is provisioned into it via the existing
  `EnsureRuntime` path.
- The private home is unconditional: there is **no external escape hatch**. The
  user cannot point agents-sandbox at another runtime, so `MSB_PATH`,
  `MSB_LIBKRUNFW_PATH`, persisted runtime paths, and the `External` /
  `Incomplete` runtime concepts are dropped. This is deliberate — supporting an
  externally configured runtime would reintroduce exactly the coupling this work
  removes. (A future opt-in could revisit it, but not now.)

### No user-facing msb

- agents-sandbox stops installing or advertising an `msb` for the user. The
  `doctor` PATH hints and "install msb for the user" behavior are removed. If
  agents-sandbox is the only thing installed, there is no `msb` command — that
  is expected, not a defect.

### Retire version-matching recovery

- The mismatch recovery menu is removed. Since our runtime is always the
  SDK-pinned one in our private home, a mismatch from the user's side cannot
  arise. Any internal sanity check that remains is a plain error, not an
  interactive recovery flow.

### Clean break, with stale-state safety

- On first run after the change, a project's recorded home volume no longer
  resolves in the private home. The code must detect "recorded volume missing in
  this home" and start fresh (create a new home volume), not error.
- The detection is by the recorded name not resolving in the private home, not by
  comparing against the legacy dir.
- The clean-break overwrite must not happen before the copy decision is made, or
  it would orphan the old data: resolving the home volume (which starts fresh)
  runs only after the offer below has been resolved. While the offer is pending
  and undismissed, a non-interactive session blocks before this step (see below)
  rather than overwriting the recorded reference.

### Opt-in copy of legacy home volumes

- Offered via a prompt (no dedicated subcommand). Offered on every interactive
  session start (`run`/`shell`) while the offer is both *necessary* and
  *undismissed*. Not offered on unrelated runtime-touching commands (`list`,
  `prune`, `volume`, `image`, ...): the offer only matters where a home volume is
  about to be used.
- The choice is **mandatory**: **copy**, **start fresh**, or **quit**. There is
  no "decide later". The session cannot proceed with the decision unmade, because
  proceeding resolves the home volume and would overwrite the project's recorded
  reference before the old data could be copied. Quitting the prompt (or a prompt
  failure) aborts the session without a decision and re-offers next time.
- Copies **all** necessary legacy home volumes in one acceptance (the offer is
  global, not per project). Names are preserved, so the existing
  project → volume mapping in `~/.local/state` resolves unchanged in the private
  home.
- Never copies binaries, `libkrunfw`, the FFI cache, the runtime database, VM
  records, or image caches — those are re-provisioned/rebuildable. The buildkit
  cache volume (`agents-sandbox-buildkit-cache`) is likewise excluded.
- The copy is a host-side byte copy: locate the source directory, copy into a
  volume created in our private home. No shell-out to any `msb`, and no throwaway
  VM — a foreign home's volume cannot be mounted by our runtime anyway.
- Source selection is by name, not by our per-project state: any directory under
  `~/.microsandbox/volumes/` matching `agents-sandbox-home-*` that is absent from
  the private home. No layout guessing beyond this convention.
- A failed copy aborts the session: continuing would orphan the old data, while
  the per-volume copy already removed any partial volume, so necessity stays true
  and the offer returns next session.
- A clean break (user chooses "Start fresh") is recorded as the dismissal: the
  legacy volumes remain, so without a persisted decision the offer would keep
  returning.
- `--yes` and non-interactive runs never accept the copy and cannot make the
  decision, so while the migration is necessary and undismissed they **block
  before starting a fresh home**, printing guidance to run an interactive
  `run`/`shell` once. `--yes` is treated as non-interactive for this offer,
  matching the precedent that `--yes` does not auto-select the (now removed)
  runtime recovery action. A multi-gigabyte copy must never be triggered
  implicitly, and the recorded reference must never be silently overwritten.

### Host-side copy mechanics

- The copy runs as the invoking user and does **not** chown on the host. A host
  `chown` would require root and a host `dev` user and is impossible on macOS, so
  it cannot be the mechanism here.
- This aligns with the rest of the design: the runner image creates `dev` with
  the **host's** uid/gid (`USER_UID`/`USER_GID` build args fed from
  `os.Getuid()`/`os.Getgid()` in `internal/sandbox/image`), so host-owned files
  and in-VM `dev` share a uid — the same reason the `/workspace` bind mount needs
  no chown. The home volume is mounted without an `Owner` override
  (`internal/sandbox/vm`), so the guest owner presented for host files comes from
  the runtime's mount default (stat-virtualization overlay / fallback owner).
- **Unverified:** that this presentation makes a copied volume readable/writable
  as `dev` end-to-end has not been exercised under KVM (see "Open verification").
  The `chown -R dev:dev` in the throwaway-VM copy path
  (`internal/sandbox/volume/volume.go`) is a **guest-side** normalization to
  `dev` after a guest-side copy — consistent with `dev` being the owner, not
  evidence of a host→guest translation.
- A recursive copy preserves file mode and modification time; symlinks are
  recreated as symlinks, never followed.
- Interruption handling: a per-volume copy that fails removes that volume
  best-effort, so the name is again absent from the private home and necessity
  stays derived true, letting the next interactive start re-offer. Previously
  completed volumes are left in place and simply stop being necessary.
- Crash safety: a copy stopped by a crash or kill runs no cleanup, so a
  half-populated private volume could otherwise look complete. A finished copy
  therefore writes a completion sentinel inside the volume, and private
  presence requires that sentinel: an unmarked private volume counts as
  still-to-copy, so an interrupted copy is re-offered. A retry discards the
  leftover partial volume before recreating it. This means the presence check is
  no longer a pure name listing for the private home (it also checks the
  sentinel); legacy necessity itself is unchanged.

### Persisted state

- Necessity is **derived** each run from the two homes: at least one directory
  matching `agents-sandbox-home-*` exists under `~/.microsandbox/volumes/` and
  its name is not a *completed* volume in the private home's `volumes/` (present
  and carrying the copy-completion sentinel). Nothing is persisted for it, and
  it does not depend on any per-project state.
- Completion is also **derived**: once copied volumes are present in the private
  home, necessity is false and no offer is made.
- The only persisted value is the user's opt-out, a single key in root
  `~/.local/state/agents-sandbox/state.yaml`:

  ```yaml
  runtime_home_migration:
    dismissed: true
  ```

  The key name is `runtime_home_migration` — scoped by `home` so it does not
  collide with the existing volume (`CmdMigrate`) or config-migration senses of
  "migration", and it keeps the codebase's "runtime" abstraction rather than
  leaking the vendor name `msb`. The inner key is `dismissed`, reusing the
  established opt-out term (`upgrade.json`'s `dismissed_versions`; onboarding's
  `state: dismissed`). It stays flat until a second axis appears.
- `dismissed` is a permanent flag with no re-arming (unlike onboarding's
  source-hash re-arm). After a dismissal, agents-sandbox never offers again;
  there is no in-product re-entry.

### State-file placement (related, separate)

- Folding launcher-global state into a single scope-based `state.yaml` is issue
  #141. This work only adds its `runtime_home_migration` key to root
  `state.yaml`; it must not introduce a feature-named file of its own.

## Left to the builder

- Exact wiring of `MSB_HOME` at startup and any interaction with the SDK FFI
  auto-loader; confirm nothing in the tool's own code still assumes
  `~/.microsandbox`.
- The precise provisioning call and where it runs in the startup sequence.
- Removing the external-runtime handling (`MSB_PATH` / `MSB_LIBKRUNFW_PATH` /
  persisted paths / `External` / `Incomplete`) and the recovery menu, while
  keeping any internal sanity checks that should remain.
- Where the copy prompt hooks into the session start (`run`/`shell`) path.
- The stale-state detection ("recorded volume missing") and where it lives.
- `doctor` changes after removing the runtime prerequisite/hints.
- macOS-specific path and behavior confirmation.
- Testing strategy, including a fake legacy home for the copy path.
- Docs and `CHANGELOG.md` updates per the repo's documentation rules.

## Open verification

- A KVM session integration test should confirm the end-to-end result: a legacy
  home volume copied host-side (no `chown`) is readable and writable by the in-VM
  `dev` user. The design makes this likely correct (build-time `dev` uid/gid
  alignment; mount owner presentation), but it is not yet verified. Cover the
  cross-uid case (an image built under one host uid reused by a runner under
  another) and the default `HostPermissions: Private` mount policy. If the test
  fails, an ownership-mapping step (e.g. an in-VM `chown -R dev:dev` on first
  use, mirroring the prefill path) must be added.

## Open question deferred to the builder

- Whether a dismissed decision should ever be reversible in-product. This design
  says no; if that proves user-hostile, the natural future shape is a subkey
  (e.g. a `completed` / timestamp alongside `dismissed`) rather than a new file.

## Relationship to other specs and issues

- Supersedes the runtime-sharing assumptions behind the mismatch-recovery
  behavior described in the troubleshooting docs.
- Depends on the state-by-scope refactor only in that its key lives in root
  `state.yaml`; the key itself can be added before #141 lands if the root file
  is created for it.
- Issue #141: organize persisted state by ownership scope.
