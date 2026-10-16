# Detached Jobs (#146) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run `dockerd` and the agent `serve` daemon as microsandbox runtime-owned detached jobs (`Sandbox.ExecDetached` + `Job`), replacing shell-backgrounded `nohup … &` processes, logfile polling, and `pkill` start/stop plumbing.

**Architecture:** Extend the `internal/sandbox/msb` seam with `ExecDetached`/`Job`/`GetJob`/`ListJobs`; give the agent `DaemonProvider` a structured `DaemonCommand` instead of shell strings; rewrite `dockerd.go`/`daemon.go` to launch jobs and derive readiness from the existing health probes plus `Job.Inspect` for early-crash detection and `Job.Logs` for failure diagnostics. Jobs die with the VM, so no job state is persisted; idempotency stays health-probe based and restart kills a rediscovered job via `ListJobs` command match.

**Tech Stack:** Go 1.26, microsandbox Go SDK `sdk/go v0.7.8` (`jobs.go`), Cobra/Viper unaffected, testify-style table tests.

**Spec:** `.operator-shared/specs/detached-jobs.md` (created in Task 6).

## Global Constraints

- No shell fallback. `JobError{Code:"unsupported_feature"}` handling is intentionally absent — trust the pinned SDK + force-reinstalled private runtime.
- No live log streaming. Verbose behaviour is unchanged; `Job.Logs` is read only on failure/timeout.
- Reuse SDK DTOs (`msbSdk.JobInfo`, `JobExit`, `JobLogEntry`, `JobLogOptions`, `JobListOption`) in the seam; do not redeclare them.
- Remove `daemonShellFunc`'s start/kill role and all `nohup`/`> …log`/`pkill -f '… serve'` strings. Retain only `pkill containerd` + pid/socket residue cleanup for dockerd.
- Lint/format with `golangci-lint` (`make fmt`, `make lint`); never `go vet`.
- Tests: unit tests with the msb mock job seam; one `//go:build integration` test in `internal/sandbox/msb`. `make compile-all` must stay green.

## Review Focus

- **A probe that never becomes healthy while the job is still alive** → timeout error that includes a `Job.Logs` tail (not a silent hang).
- **A job that exits immediately (bad binary/args)** → structured failure naming the exit code + log tail, not a 30s timeout.
- **A start when an old dockerd/daemon job is still alive** → the stale job is killed (matched by command) before a new one starts, so no duplicate bind on `:4096`/the socket.
- **`restartDaemons` after a config change** → forces a restart even though the daemon is currently healthy.
- **A job whose `Close` is called** → never terminates the process (Close is handle-only).
- **The `serveOnly` hostname** → the detached job binds `0.0.0.0`, else host port-forwarding gets an empty reply.
- **An agent without a `DaemonProvider`** → `ensureDaemon` is a no-op and issues no job/shell calls.

---

### Task 1: msb seam — `ExecDetached`, `Job`, `GetJob`, `ListJobs`

**Files:**
- Modify: `internal/sandbox/msb/msb.go`
- Modify: `internal/sandbox/msb/testmock.go`
- Test: `internal/sandbox/msb/msb_jobs_test.go` (create)

**Interfaces:**
- Produces:
  - `type Job interface { ID() string; Inspect(ctx context.Context) (*msbSdk.JobInfo, error); Kill(ctx context.Context) error; Signal(ctx context.Context, signal int) error; Logs(ctx context.Context, options msbSdk.JobLogOptions) ([]msbSdk.JobLogEntry, error); Close() error }`
  - `Sandbox.ExecDetached(ctx context.Context, cmd string, args []string, opts ...msbSdk.JobExecOption) (Job, error)`
  - `Sandbox.GetJob(ctx context.Context, id string) (Job, error)`
  - `Sandbox.ListJobs(ctx context.Context, opts ...msbSdk.JobListOption) ([]msbSdk.JobInfo, error)`
  - `MockJob` test double and `MockSandbox.ExecDetached`/`GetJob`/`ListJobs` (+ `ExecDetachedFn`, `ExecDetachedCalls *[]MockDetachedCall`, `Job_ Job`, `Jobs []msbSdk.JobInfo`, `GetJobFn`, `ListJobsFn`).

- [ ] **Step 1: Write failing tests** in `internal/sandbox/msb/msb_jobs_test.go`: `TestMockSandboxExecDetachedRecordsCall` (asserts `ExecDetachedCalls` records `{Cmd,Args,User}` from opts), `TestMockSandboxListJobsReturnsConfigured`, `TestMockSandboxGetJobReturnsJob`, `TestMockJobKillSetsDidKill` and `TestMockJobLogsReturnsEntries`. Build them against the new fields (which do not exist → compile failure is the red).
- [ ] **Step 2: Run** `go test ./internal/sandbox/msb/ -run TestMockSandbox.*Jobs -v` → FAIL (undefined).
- [ ] **Step 3: Implement the seam** — add the `Job` interface and the three `Sandbox` methods; add real adapters (`realJob` carrying a `*msbSdk.Job`) and package-level seams `realSandboxExecDetached`, `realSandboxGetJob`, `realSandboxListJobs`, `realJobID/Inspect/Kill/Signal/Logs/Close` (method expressions), matching the existing adapter pattern in `msb.go`. `ListJobs` paginates internally like `ListSandboxes` (loop on `page.NextCursor` via `msbSdk.WithJobCursor`). Add `MockJob`, `MockDetachedCall`, and the mock `Sandbox` methods to `testmock.go`; default `ExecDetached` with no `ExecDetachedFn` returns a configured `Job_` (else a fresh `&MockJob{}`).
- [ ] **Step 4: Run** the tests → PASS; run `go build ./...`.
- [ ] **Step 5: Commit** `feat(msb): add detached-job seam (ExecDetached/Job/GetJob/ListJobs)`.

---

### Task 2: Structured agent daemon command

**Files:**
- Modify: `internal/agent/capabilities.go`, `internal/agent/opencode.go`, `internal/agent/opencode2.go`
- Test: `internal/agent/opencode_test.go`, `internal/agent/opencode2_test.go`

**Interfaces:**
- Produces: `type DaemonCommand struct { Binary string; Args []string }`; `DaemonProvider.DaemonCommand(serveOnly bool) DaemonCommand` replaces `DaemonStartCmd`/`DaemonKillCmd`. `DaemonHealthCmd`/`DaemonHealthParse` unchanged.
- Consumes: nothing.

- [ ] **Step 1: Rewrite the failing tests** — replace `TestOpencodeDaemonStartCmd` with `TestOpencodeDaemonCommand` (assert `Binary=="opencode"`; args contain `serve`; `--hostname 127.0.0.1` local, `0.0.0.0` serve-only) and `TestOpencodeDaemonHealthCmd` (drop kill assertions). Mirror in `opencode2_test.go` asserting `Binary=="opencode2"`, `--port 4096`. Run → FAIL.
- [ ] **Step 2: Run** `go test ./internal/agent/ -run Daemon -v` → FAIL.
- [ ] **Step 3: Implement** `DaemonCommand`/`DaemonProvider.DaemonCommand`, delete `DaemonStartCmd`/`DaemonKillCmd` from the interface and both profiles (opencode2 log-file assertion goes away).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `refactor(agent): expose daemon as structured command, drop shell strings`.

---

### Task 3: dockerd as a detached job

**Files:**
- Modify: `internal/sandbox/vm/dockerd.go`
- Create: `internal/sandbox/vm/detached.go`
- Test: `internal/sandbox/vm/dockerd_test.go`, `internal/sandbox/vm/dockerd_error_test.go`, `internal/sandbox/vm/coverage_edge_test.go`, `internal/sandbox/vm/detached_test.go` (create)

**Interfaces:**
- Consumes: Task 1 (`msb.Sandbox.ExecDetached`, `msb.Job`, `ListJobs`, `GetJob`).
- Produces (in `detached.go`):
  - `func jobExited(info *msbSdk.JobInfo) (int, bool)` — true when `info.ExitCode != nil`.
  - `func jobTailLogs(ctx context.Context, job msb.Job) string` — `job.Logs` with a `Tail` of 100, joined.
  - `func killJobsByCommand(ctx context.Context, sb msb.Sandbox, binary string, args []string, ui termio.UI)` — `ListJobs(msbSdk.WithAllJobs())`, match prefix `binary+args`, `GetJob` then `Kill` (log failures as verbose).
  - `func awaitJobReady(ctx, sb, job, probeCmd, probeUser, label string, timeout, poll time.Duration, ui) error` — loop: `Inspect` → if exited return crash error; probe; on timeout return error including `jobTailLogs`.
  - `var dockerdCommand = struct{ Binary string; Args []string }{...}` set to `dockerd -H unix:///var/run/docker.sock --storage-driver=vfs`.
- Produces (in `dockerd.go`): `dockerdCleanupCmd` (replaces `dockerdRestartCmd`; `pkill containerd` + pid/sock delete, no `pkill dockerd`, no backgrounding).

- [ ] **Step 1: Write failing tests**:
  - `dockerd_test.go`: rewrite `countingSandbox` around `ExecDetached`/`ListJobs`; keep `TestStartDockerdIfPresentNoDockerdBinary`, `…AlreadyRunning` (no `ExecDetached` call), `…StartsAndBecomesReady` (one `ExecDetached`, job stays alive, probe becomes healthy), `…NeverReady` (timeout), `…ProbesBeforeWaiting`. Replace `TestDockerdRestartCmdTearsDownContainerd` with `TestDockerdCleanupCmdRemovesContainerdResidue` (asserts `pkill containerd`, `.sock`, `.pid`; asserts **no** `pkill dockerd`, **no** trailing `&`). Replace `TestTimeoutErrorIncludesDockerdLog` with `TestTimeoutErrorIncludesJobLog` (mock job logs embed a sentinel).
  - `detached_test.go`: `TestAwaitJobReadyCrashReturnsExitAndLogs` (mock `Inspect` reports `ExitCode`), `TestKillJobsByCommandMatchesAndKills`, `TestKillJobsByCommandIgnoresNonMatch`.
- [ ] **Step 2: Run** `go test ./internal/sandbox/vm/ -run 'Dockerd|Detached|KillJobs|AwaitJob' -v` → FAIL.
- [ ] **Step 3: Implement** `detached.go` helpers and rewrite `startDockerdIfPresent` per the agreed flow (binary check → health probe → `killJobsByCommand` → `dockerdCleanupCmd` shell → `ExecDetached` as `RootUser` → `awaitJobReady` with `dockerdReadyCmd`, `DefaultSandboxUser`, `dockerdReadyTimeout`, `dockerdPollInterval`). Delete `/var/log/dockerd.log` handling and `dockerdRestartCmd`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(vm): run dockerd as a runtime-owned detached job`.

---

### Task 4: agent serve daemon as a detached job

**Files:**
- Modify: `internal/sandbox/vm/daemon.go`, `internal/sandbox/vm/reconfig.go`
- Test: `internal/sandbox/vm/daemon_test.go`, `internal/sandbox/vm/reconfig_error_test.go`, `internal/sandbox/vm/setupsandbox_test.go`, `internal/sandbox/vm/create_project_vm_test.go`, `internal/sandbox/vm/coverage_more_test.go`, `internal/sandbox/vm/coverage_extra_test.go`, `internal/sandbox/vm/coverage_dryrun_test.go`, `internal/sandbox/vm/prepare_sandbox_test.go`, `internal/sandbox/vm/workspace_guest_test.go`, `cmd/agents-sandbox/cli_run_shell_test.go`

**Interfaces:**
- Consumes: Task 2 (`DaemonProvider.DaemonCommand`), Task 3 (`killJobsByCommand`, `awaitJobReady`).
- Produces:
  - `func ensureDaemon(ctx context.Context, a agent.Agent, serveOnly, force bool, sb msb.Sandbox, ui termio.UI) error` — no-op without provider; return early if `!force && healthy`; else kill stale job by `DaemonCommand`, `ExecDetached` (default user), `awaitJobReady` with `DaemonHealthCmd`, `daemonReadyTimeout`/`daemonPollInterval`, label `<name> daemon`.
  - `func SetDaemonHealthFunc(f func(ctx context.Context, sb msb.Sandbox, command string) (string, int, error)) func(...)` — replaces `SetDaemonShellFunc`; only health probing goes through it (`daemonHealthFunc`). Delete `daemonShellFunc`/`SetDaemonShellFunc`.

- [ ] **Step 1: Write failing tests**:
  - `daemon_test.go`: keep `TestEnsureDaemonNoDaemonProvider`; rewrite the others to pass a `MockSandbox` with a `MockJob` and drive health via `SetDaemonHealthFunc`; add `TestEnsureDaemonStartsDetachedCommand` (assert `ExecDetachedCalls` binary `opencode`, args on `127.0.0.1`), `TestEnsureDaemonServeOnlyBindsExternalInterface`, `TestEnsureDaemonForceRestartsHealthy` (force=true still restarts), `TestEnsureDaemonKillsStaleJobByCommand`, `TestEnsureDaemonCrashSurfacesExitAndLogs`, `TestEnsureDaemonFailsAfterTimeout`. Update `daemonReadyTimeout`/`daemonPollInterval` overrides as today.
  - `reconfig_error_test.go`: replace `TestDefaultDaemonShellFuncError`/`Success` with `TestDefaultDaemonHealthFuncError`/`Success`; update `TestRestartDaemonsEnsureDaemonError` to the new signature.
  - `setupsandbox_test.go`: `TestRestartDaemonsRestartsServe` now asserts a job kill (via `MockSandbox` `ExecDetachedCalls`/job `DidKill`) rather than `DaemonKillCmd`; assert `dockerdCleanupCmd`/`dockerdCommand` was not used.
  - Update every remaining `SetDaemonShellFunc` caller (`create_project_vm_test.go`, `coverage_*`, `prepare_sandbox_test.go`, `workspace_guest_test.go`) to `SetDaemonHealthFunc` and pass a non-nil `MockSandbox`.
  - `cmd/agents-sandbox/cli_run_shell_test.go`: `setupRunMocks` calls `sandbox.SetDaemonHealthFunc`.
- [ ] **Step 2: Run** `go test ./internal/sandbox/vm/ ./cmd/agents-sandbox/ -run Daemon -v` → FAIL.
- [ ] **Step 3: Implement** the `daemon.go` rewrite and update `reconfig.go` (`ensureDaemon(..., force)`: `setUpSandbox` uses `false`, `restartDaemons` uses `true`) and all test callers.
- [ ] **Step 4: Run** `go test ./... && make compile-all` → PASS.
- [ ] **Step 5: Commit** `feat(vm): run agent serve daemon as a runtime-owned detached job`.

---

### Task 5: integration test for the jobs round-trip

**Files:**
- Create: `internal/sandbox/msb/jobs_integration_test.go`
- Modify: `Makefile` only if a dedicated per-test env gate is needed (default: reuse `make test-integration`).

**Interfaces:**
- Consumes: Task 1 seam, `msbSdk` (`CreateSandbox`, `WithImage`, `WithReplace`, `WithDetached`).
- Produces: `TestJobsRoundTrip` (`//go:build integration`) — creates a real sandbox (small public image), `ExecDetached` a short `sh -c 'echo <sentinel>; sleep 30'`, asserts `ID()` non-empty, `Inspect` reaches a running state, `ListJobs(WithAllJobs)` contains the id, `Logs` contains the sentinel, `Kill` then `Inspect` shows a finished/`ExitCode`, and `Close` is called. Cleans up the sandbox in `t.Cleanup`.

- [ ] **Step 1: Write the test** as above; it must compile under `-tags integration`.
- [ ] **Step 2: Run** `go vet`? No — run `make compile-all` (compiles tagged files without a daemon) → PASS. Live run is `make test-integration` on a KVM host (documented in Task 6).
- [ ] **Step 3: Commit** `test(msb): integration coverage for detached-job round-trip`.

---

### Task 6: docs, changelog, spec, brain

**Files:**
- Create: `.operator-shared/specs/detached-jobs.md`
- Modify: `docs/runner-image.md` (Docker-in-sandbox lifecycle wording: job-owned, no logfile), `docs/diagrams/lifecycle.puml` if it references shell backgrounding, `CHANGELOG.md` `[Unreleased]`
- Modify: `.operator-shared/catalog.md` (add the spec entry)

**Interfaces:** none (documentation).

- [ ] **Step 1: Write the spec** `.operator-shared/specs/detached-jobs.md`: the detached-job contract (runtime-owned jobs for dockerd + serve daemon; no shell fallback; health-probe readiness + `Inspect` crash detection + `Logs` on failure; kill-by-command rediscovery; no persisted job IDs; no live log streaming; dockerd residue cleanup retained) with `description`/`read_if` frontmatter.
- [ ] **Step 2: Update docs/changelog** — `docs/runner-image.md` states dockerd is launched as a runtime-owned job and its log is read via `Job.Logs` on failure; add a `CHANGELOG.md` `[Unreleased]` line; catalog the new spec.
- [ ] **Step 3: Run** `make check` (fmt, lint, test, compile-all, docs-linkcheck).
- [ ] **Step 4: Commit** `docs: document detached-job runtime for dockerd and agent serve`.

---

## Host verification checklist (manual, post-branch)

Run on a KVM host with a runner image containing dockerd + opencode:

1. `agents-sandbox run --docker` in a project → dockerd healthy; `docker ps` inside works; `msb jobs`/`SandboxHandle.ListJobs` shows a `dockerd` job.
2. Edit the agent config → run again → observe the serve job is killed and re-created (no duplicate `:4096` bind).
3. Stop and start the VM → both jobs re-launch.
4. `make test-integration` → `TestJobsRoundTrip` passes.
5. Point at a broken dockerd (e.g. temporary bad arg) → launcher fails fast with the exit code + log tail, not a 30s timeout.
