# Shared Partition Catalog

## Tree

- `operator.md`
  - Description: Operator Instructions for this partition. Currently a stub; rules/style remain private and `AGENTS.md` is authoritative.
  - Read If: Auto-injected.
- `catalog.md`
  - Description: This catalog.
  - Read If: Auto-injected.
- `README.md`
  - Description: Operator Memory overview and install instructions.
  - Read If: Onboarding to Operator / setting up the plugin.

### `specs/` - System contracts for this partition

- `runtime-home-decoupling.md`
  - Description: Design for decoupling agents-sandbox's microsandbox runtime home from any user-installed microsandbox: private XDG data home (`~/.local/share/agents-sandbox/msb`), no user-facing msb, retired version-matching recovery, clean break with stale-state safety, and an opt-in copy of legacy home volumes. Copy progress is tracked host-side in a `migrations` registry (`runtime_home` entry: `copying`/`completed`/`dismissed` + transient `copied`/`kept`/`excluded`) in root `state.yaml`, under a host-only flock; the in-volume sentinel is retired. A second review round added the "no unusable partial" invariant, the ambiguous-state offer (present private volumes of unclear status, `volume remove [--force]` cleanup path), the `Start fresh`/`Dismiss` label rule, and a finalization warning for uncopied volumes; implemented on `opencode/runtime-home-decoupling` (pending merge).
  - Read If: Implementing or reviewing runtime-home decoupling, the private `MSB_HOME`/`EnsureRuntime` wiring, removal of the mismatch recovery menu, doctor/PATH changes, the legacy home-volume copy state model (resume/keep/exclusion/concurrency), the ambiguous-state offer, or the `volume remove` cleanup command.
- `runner-image-build.md`
  - Description: Agent version selection precedence (`--agent-version`, upgrade check, user-provided), when the runner image is (re)built, and when `BuildOptions.Force` bypasses the Docker layer cache.
  - Read If: Changing image build/caching, `BuildOptions`, `--rebuild`, `--agent-version`, agent upgrade, or volume subcommands' image handling.
- `guided-onboarding.md`
  - Description: Design contract for first-run onboarding: credential kinds (static vs login; never placeholder-migrate refreshable OAuth), host determination, derived onboarding state, always-explicit `Guide` outcomes, the tiered setup wizard, and `config harden`.
  - Read If: Working on config migration, first-run guidance, credential/auth handling, network egress derivation, the setup wizard, or `config harden`.
- `termio-ui-backends.md`
  - Description: Contract for termio's UI backends: the decision to confine huh to modal prompts (spinners/tables/log output stay hand-rolled), the `termio.UI` mockability seam, the `OPENCODE_SANDBOX_PROMPT` prompt switch, the accessibility/non-TTY invariant (huh implements accessible by bypassing bubbletea), and why the huh spinner was rejected (bubbletea diff renderer corrupts under concurrent output).
  - Read If: Changing interactive prompts or spinners, scoping a UI library (huh, bubbletea, ...), or reasoning about mockability, non-TTY/accessibility, or concurrent-output behavior.
- `image-composition.md`
  - Description: Contract for composing the per-project runner Dockerfile into per-tool multistage stages (base/toolbase/node/agent/docker) merged into the runner via `COPY`, so tool-owned layers cache independently of the user body, the user base, and each other; the tool-owned `agents-sandbox-toolbase` (`debian:trixie-slim` + `curl`/`ca-certificates`/`tar`) the node/agent/docker stages build on so a custom base needs no `curl`/`tar`; when the node stage is emitted (only for agents that require node, i.e. pi), native-installer vs npm agent install methods, the `/opt/agents-sandbox` artifact contract, unconditional install with adoption-time provenance, the docker marker as a copy/adopt point, the `iptables git ps xz` docker runtime-prerequisite check, forcing vfs via the dockerd flag instead of `daemon.json`, the `dind` → `docker` rename with deprecated aliases, and login-shell PATH handling (`AGENTS_SANDBOX_IMAGE_PATH` re-merged via the `/etc/profile.d` script, whose content is folded into the dockerfile-id).
  - Read If: Changing `RenderDockerfile`/stage composition, which agents need node, agent install methods, tool install paths, layer caching, the docker/dind switch or marker, custom-base handling, login-shell PATH handling (`AGENTS_SANDBOX_IMAGE_PATH`, the `/etc/profile.d` merge script, dockerfile-id identity), or the dockerd storage driver, or the integration docker-build tests' use of the classic builder cache.
- `login-shell-path.md`
  - Description: Contract for keeping the runner image's composed PATH (custom Dockerfile `ENV PATH`, `/opt/agents-sandbox/bin`, project toolchains) effective in login-shell contexts by re-merging dropped entries from `AGENTS_SANDBOX_IMAGE_PATH` through a `/etc/profile.d` drop-in (devcontainers-style mergePaths); supersedes the `/usr/local/bin` symlink stopgap. Acceptance target is resolution equivalence, not byte-identical PATH.
  - Read If: Working on runner-image PATH handling, login-shell (`-l`) sessions, `run`/`shell` attach, custom Dockerfile `ENV PATH`, or the `/etc/profile.d` merge helper.
- `buildkit-builder-vm.md`
  - Description: Design for issue #15: build the runner image without host Docker by running `buildkitd` inside a microsandbox VM, driven by a host-side BuildKit Go client over a published TCP port; free/unique host port invariant, `native` snapshotter, wildcard guest bind, named-volume cache, `ImageLoad` delivery, identity/reuse via the msb cache, removal of `internal/sandbox/docker` + `doctor.CheckDocker`, registry auth via the BuildKit session, and open decisions (base-digest resolver, export format, builder VM reuse, cache scope, credential source). Validated by `spike/buildkit-vm/`.
  - Read If: Implementing or reviewing the Docker-free BuildKit builder (issue #15), the `internal/sandbox/builder` package, or changes to the image build/inspect/load pipeline and doctor prerequisites.
- `runtime-management-commands.md`
  - Description: Contract for extending the CLI to cover microsandbox's *management* surface against the private runtime home (Slice 0 implemented, PR #174, pending merge): `volume inspect`, `image inspect`/`remove`, `sandbox remove`/`inspect`, a top-level `df` plus a `storage` group alias, and uniform `-q`/`--format json`/`--yes`/`--older-than` conventions. Ordered delivery slices (0 shared CLI conventions, 1 image reference-guard seam, 2 accounting seam over microsandbox's `StorageUsage`, 3 inspection, 4 removal, 5 `df`/`storage`), delivered as sub-issues #168–#172 and #155; slice 6 (`sandbox copy`) dropped as the excluded data-portability surface. Explicitly excludes msb's lifecycle, exec/logs/metrics, pull/load/save, registry, cloud, and self-install surfaces, and `sandbox compact` (#157); runtime-RAM pruning (#156) is related, not excluded.
  - Read If: Adding or reviewing volume/image/sandbox inspection and removal commands, the `df`/`storage` accounting commands, prune confirmation/format flags, or command-surface consistency conventions.
- `sandbox-identity.md`
  - Description: Contract distinguishing a sandbox's mutable *name* from its stable *identity*. Removal/attach fail closed via the SDK's identity-aware handle/live operations and `ErrSandboxReplaced` (classified by a new `msb.IsReplaced`), handled per path (prune skip-and-report, task sandboxes destroy, reconfig-replace → recreate if already gone but fail closed if replaced, reaper → stop). Retires the name-based `msb.Client.RemoveSandbox`; adds `Sandbox.Destroy` to the seam. No persisted ID, no state migration; no `ID()` accessor needed on the seam for removal/attach (later Slice 3 adds `SandboxHandle.ID` purely for `sandbox inspect` display). Defense-in-depth for #144, which is a separate naming concern. Implemented in #158 (merged).
  - Read If: Working on the msb seam's sandbox removal/attach paths, prune, the reaper, reconfig-replace, or reasoning about name-vs-identity safety, `ErrSandboxReplaced`, or #144 vs #158.

### `plans/` - TDD implementation plans

- `2026-10-07-guided-onboarding-a-credential-kind.md`
  - Description: Plan A (credential kinds, explicit `Guide`, network-deny handling) — implemented on `issue-99-guided-onboarding-a`.
  - Read If: Reviewing or resuming Plan A of `specs/guided-onboarding.md`.
- `2026-10-07-guided-onboarding-b-state-machine.md`
  - Description: Plan B (onboarding state machine: persisted `manual`/`login-pending`, verification loop, non-interactive reminder) — implemented on `issue-99-guided-onboarding-b`.
  - Read If: Implementing or resuming Plan B of `specs/guided-onboarding.md`.
- `2026-10-08-guided-onboarding-c-tiered-wizard.md`
  - Description: Plan C (tiered setup wizard: `internal/configfile` writer extraction, `internal/configwizard` catalog/runner/state, viperconfig higher-scope detection, `config wizard [--tier=N]`, first-run integration) — implemented and merged into `issue-99-guided-config-setup`.
  - Read If: Implementing or resuming Plan C of `specs/guided-onboarding.md`, or working on the setup wizard.
- `2026-10-09-login-shell-path.md`
  - Description: Plan for `specs/login-shell-path.md`: the POSIX-sh profile.d merge script (+ sh behaviour tests), image wiring (`AGENTS_SANDBOX_IMAGE_PATH`, build-context `COPY`), removing the `/usr/local/bin` symlink stopgap, an end-to-end login-shell integration test, and docs/spec/CHANGELOG updates — implemented on `opencode/speed-up-image-builds` (PR #126).
  - Read If: Reviewing the login-shell PATH (mergePaths) work; the plan is complete.
- `2026-10-10-buildkit-builder-vm.md`
  - Description: TDD plan for `specs/buildkit-builder-vm.md` (issue #15): new `internal/sandbox/builder` package (shared builder VM lifecycle, host-side BuildKit client seam/readiness, solve+OCI export+metadata, gateway `ResolveImageConfig` base-digest route, registry auth), add the `moby/buildkit`/`docker/cli` deps, rewire `image` onto the builder, thread the archive through VM creation, BuildKit-cache pruning, remove `internal/sandbox/docker` + `doctor.CheckDocker`, integration + docs. Not yet implemented. Resolves the spec's open decisions (OCI export, `~/.docker/config.json` creds, global cache) and overrides the spec's buildinfo base-digest route.
  - Read If: Implementing or reviewing the Docker-free BuildKit builder (issue #15 / `internal/sandbox/builder`).
- `2026-10-13-runtime-home-decoupling.md`
  - Description: TDD plan for `specs/runtime-home-decoupling.md`: `configpaths.UserDataDir`, private `MSB_HOME` + removal of external runtime resolution, retirement of the version-mismatch recovery menu, doctor msb-advertising removal, root `state.yaml` accessor (`runtime_home_migration.dismissed`), legacy home-volume necessity/copy, and the session-start copy prompt. Implemented on `opencode/runtime-home-decoupling`; only the ownership-mapping verification remains. The copy state model it defined is superseded by `2026-10-15-runtime-home-migration-state.md`.
  - Read If: Reviewing or resuming runtime-home decoupling (`opencode/runtime-home-decoupling`).
- `2026-10-15-runtime-home-migration-state.md`
  - Description: TDD plan implementing the first PR #142 review-driven revision in `specs/runtime-home-decoupling.md`: replace the unsafe in-volume copy sentinel with a host-owned `migrations` registry (status `copying`/`completed`/`dismissed` + transient `copied`/`excluded`), status-aware necessity, a host-only flock copy campaign with exact resume and auto-exclusion, and Offer wiring. Implemented; the copy state model is refined by `2026-10-15-runtime-home-migration-state-review-fixes.md`.
  - Read If: Reviewing the first legacy home-volume copy state model, resume/exclusion, or cross-process copy concurrency.
- `2026-10-15-runtime-home-migration-state-review-fixes.md`
  - Description: Second PR #142 review round. Splits the transient `excluded` set into `kept` (user-dismissed, destination left as-is) and `excluded` (copy failure, destination removed); adds the "no unusable partial" invariant (never complete while a usable partial remains; dismiss removes unclear partials but preserves `copied`/`kept`), the ambiguous-state offer (present private volumes of unclear status with copy-missing/Dismiss/Quit and prompt instructions), the `Start fresh`/`Dismiss` label rule, the `volume remove [--force]` command with a fail-closed state reference guard, and a finalization warning. Implemented on `opencode/runtime-home-decoupling`.
  - Read If: Implementing or reviewing the ambiguous-state offer, the `copied`/`kept`/`excluded` split, dismiss cleanup, the `volume remove` command, or the finalization warning.
- `2026-10-15-runtime-management-slice-0.md`
  - Description: Plan for issue #168 (Slice 0 of `specs/runtime-management-commands.md`): the shared list-output seam (table / `--quiet` identifiers / `--format json`) with `termio.UI.Print` bypassing quiet suppression, wiring `sandbox`/`volume`/`image` list (deprecating `sandbox list --names` in favour of `--quiet`), the canonical `--older-than` age flag (deprecating `--age`), prune reports returned by the `pruning` package and rendered by the CLI (human or JSON), and the `termio.UI.Confirm` helper wired into prune (`--yes` skips; non-interactive without `--yes` fails fast). Locked decisions: reuse global `--quiet` as identifiers-only on lists, wire prune confirmation now, prune JSON as a structured object. Split into PR A (Tasks 1–4) and PR B (Tasks 5–7). Implemented on `issue-168-runtime-management-slice-0` (9 commits + a final review-fix commit: partial prune reports render before the joined error, `runGroupPrune` dedup, `termio.Mock.Confirm` mirrors the non-interactive `ErrConfirmationRequired` sentinel, top-level verbose detail parity, `stale_for_seconds` assertion).
  - Read If: Implementing or reviewing Slice 0 of the runtime-management epic (#168), the shared list-output helper, prune confirmation, or prune JSON output.
- `2026-10-15-runtime-management-slice-2.md`
  - Description: Plan for issue #170 (Slice 2 of `specs/runtime-management-commands.md`): the new `internal/sandbox/storage` seam (`Observe` over the six microsandbox categories with nil-as-unknown, `ErrUnsupportedOperation` degrading to unknown, plus a `runtime` host category walking the private home's `bin`/`lib`) and the `msb.Client.StorageUsage` + `IsUnsupportedOperation` seam extensions. Locked scope decision (with the user): Docker images and the BuildKit cache are deferred out of this seam (the cache is a named msb volume already in the `volumes` category; host Docker's store is outside the private home). Implemented on `issue-170-runtime-management-slice-2`.
  - Read If: Reviewing or resuming Slice 2 of the runtime-management epic (#170), the `internal/sandbox/storage` seam, or the msb `StorageUsage` addition.
- `2026-10-15-runtime-management-slice-3.md`
  - Description: Plan for issue #171 (Slice 3 of `specs/runtime-management-commands.md`): read-only `volume inspect`/`image inspect`/`sandbox inspect`. Package seams `volume.InspectVolume`/`image.InspectImage`/`vm.InspectSandbox` return local detail DTOs; the CLI renders them through a shared `printInspect` (identifier / JSON / key-value block). Two `msb` seam additions: `SandboxHandle.ID()` and `Client.ImageInspectDetail` (a local `msb.ImageDetail` over the existing `realImageInspect` seam, leaving the config-only `ImageInspect` untouched). Implemented on `issue-171-runtime-management-slice-3`.
  - Read If: Reviewing or resuming Slice 3 of the runtime-management epic (#171), the inspect commands, or the `msb` ID/ImageInspectDetail seam additions.
- `2026-10-15-custom-base-toolbase.md`
  - Description: TDD plan for #143: introduce a tool-owned `agents-sandbox-toolbase` (`debian:trixie-slim` + `curl`/`ca-certificates`/`tar`) as the parent of the node/agent/docker stages, rewire them off the user's base, remove the now-dead `command -v` install probes, drop `curl`/`tar` from the docker runtime-prerequisite check, and cover a custom base without `curl`/`tar` in integration. Implemented on `issue-143-custom-base-toolbase`.
  - Read If: Implementing or reviewing #143 / the tool-base decoupling.
