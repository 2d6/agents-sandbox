---
description: Design for decoupling agents-sandbox's microsandbox runtime home from any user-installed microsandbox: a private XDG data home, no user-facing msb, retired version-matching recovery, and an opt-in copy of legacy home volumes recorded as `runtime_home_migration.dismissed` in root `state.yaml`.
read_if: Implementing or reviewing runtime-home decoupling, the private `MSB_HOME`, removal of the runtime mismatch recovery menu, or the legacy home-volume copy prompt.
---

# Runtime home decoupling

Date: 2026-10-13

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
  this home" and start fresh (create a new home volume), not error. This keeps
  every existing user working with no migration.
- The detection is by the recorded name not resolving in the private home, not by
  comparing against the legacy dir. It is independent of the copy offer below.

### Opt-in copy of legacy home volumes

- Offered via a prompt (no dedicated subcommand). Offered on every interactive
  session start (`run`/`shell`) while the offer is both *necessary* and
  *undismissed*. Not offered on unrelated runtime-touching commands (`list`,
  `prune`, `volume`, `image`, ...): the offer only matters where a home volume is
  about to be used.
- Copies **all** necessary legacy home volumes in one acceptance (the offer is
  global, not per project). Names are preserved, so the existing
  project → volume mapping in `~/.local/state` resolves unchanged in the private
  home.
- Never copies binaries, `libkrunfw`, the FFI cache, the runtime database, VM
  records, or image caches — those are re-provisioned/rebuildable. The buildkit
  cache volume (`agents-sandbox-buildkit-cache`) is likewise excluded.
- The copy is a host-side byte copy: locate the source directory, copy into a
  volume created in our private home, then fix ownership (matching the existing
  `cp -a` + `chown` the throwaway-VM copy path uses). No shell-out to any `msb`,
  and no throwaway VM — a foreign home's volume cannot be mounted by our runtime
  anyway.
- Source selection is by name, not by our per-project state: any directory under
  `~/.microsandbox/volumes/` matching `agents-sandbox-home-*` that is absent from
  the private home. This keeps necessity and completion pure functions of the two
  directory listings and means the copy offer survives the clean-break overwrite
  of a project's recorded volume name. No layout guessing beyond this convention.
- A clean break (user starts fresh, declines the copy) is recorded as the same
  dismissal: the legacy volumes remain, so without a persisted decision the offer
  would keep returning.
- `--yes` and non-interactive runs never accept the copy: print a one-line
  reminder and continue. `--yes` is treated as non-interactive for this offer,
  matching the precedent that `--yes` does not auto-select the (now removed)
  runtime recovery action. A multi-gigabyte copy must never be triggered
  implicitly.

### Persisted state

- Necessity is **derived** each run, purely from the two directory listings: at
  least one directory matching `agents-sandbox-home-*` exists under
  `~/.microsandbox/volumes/` and is absent from the private home's `volumes/`.
  Nothing is persisted for it, and it does not depend on any per-project state.
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
- The host-side copy implementation: atomicity/interruption behavior, ownership
  fix details per platform, and how a partially copied set is handled.
- The stale-state detection ("recorded volume missing") and where it lives.
- `doctor` changes after removing the runtime prerequisite/hints.
- macOS-specific path and behavior confirmation.
- Testing strategy, including a fake legacy home for the copy path.
- Docs and `CHANGELOG.md` updates per the repo's documentation rules.

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
