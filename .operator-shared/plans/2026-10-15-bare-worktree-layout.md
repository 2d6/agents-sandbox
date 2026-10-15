# Bare Worktree Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a session started inside a git worktree bind the smallest enclosing checkout at `/workspace` and start the agent in that worktree, so a bare checkout whose worktrees live inside it works host-and-VM-consistently.

**Architecture:** One new pure function in `internal/git` resolves the bind root and outside-worktree flag from the CWD (walk up to the worktree's `.git`, derive the checkout root from the existing `resolveGitDir` helper). The VM package binds that root and derives the guest CWD; the session package passes the guest CWD to attach via the SDK's `WithAttachCwd`. No identity/naming change, no reconfig change.

**Tech Stack:** Go 1.26, go-git (already a dependency; not used in the new resolver), microsandbox Go SDK `WithAttachCwd`/`AttachWith`, testify-free table tests.

**Spec:** `.operator-shared/specs/bare-worktree-layout.md`

## Global Constraints

- The **only** product-code behavior change is *which host directory is bound at `/workspace`* plus *where the agent starts* and the outside-worktree warning. No new abstraction beyond the resolver and the plumbing.
- Derivation uses the existing plain-file inspection (`resolveGitDir`); **never** shell out to a `git` binary at runtime (tests may use `testutil.RunGit`).
- VM/home-volume/state/image **identity is unchanged** — do not touch `ProjectSlug`, `projectSlugAt`, `state.Key`, `projectVMName`, `HomeVolumeName`, `internal/sandbox/naming`, or add scoping/migration.
- The bind *source* stays a creation-time property: **do not** add bind-source-change detection to reconfig. The change applies to fresh VMs only.
- Outside-checkout worktrees keep today's binding (the CWD) and get a `termio` warning; they are **not** given their own VM or extra mounts.
- Guest CWD is `/workspace` at the bind root, `/workspace/<rel>` in a subdirectory; for an outside worktree `bindRoot == cwd`, so it stays `/workspace`.
- Guest paths are POSIX (`path` package), host paths use `path/filepath`.
- Regenerate/format with `make fmt` and verify with `make lint`; run `make test`/`go test ./...`. Follow `AGENTS.md` (never `go vet`, never hand-format).

## Review Focus

Inputs/conditions the spec implies but no single test fully pins, most likely to bite first; each gets its test in the owning task.

- A linked worktree whose `.git` file records the shared gitdir with `worktree.useRelativePaths` (relative path) — the checkout root must resolve, task 1.
- A CWD equal to the bare root itself (no working tree) — must bind the bare root without a warning, task 1.
- A CWD deeper than one level inside a worktree/subdirectory — the guest CWD must be the full relative path, task 2.
- An already-running VM on a fresh `guestCWD` — the bind source is not re-derived; only a newly created VM uses the new root, task 2.
- A non-root attach (shell path) — the CWD must still be applied through `AttachWith`, task 3.

---

### Task 1: Resolve the workspace bind layout in `internal/git`

**Files:**
- Create: `internal/git/workspace.go`
- Test: `internal/git/workspace_test.go`

**Interfaces:**
- Consumes: `resolveGitDir(root string) string` (existing, `git.go:126`).
- Produces:
  ```go
  // internal/git/workspace.go
  type WorkspaceLayout struct {
      BindRoot        string // host dir to bind at /workspace
      CheckoutRoot    string // enclosing checkout root; "" when cwd is not in a repo
      OutsideWorktree bool   // cwd is a linked worktree outside CheckoutRoot
  }
  func ResolveWorkspaceLayout(cwd string) WorkspaceLayout
  ```

- [ ] **Step 1: Write the failing test** — `internal/git/workspace_test.go`

Use `testutil.InitRepo(t)` / `testutil.RunGit`. Cover:
- non-repo `t.TempDir()` → `BindRoot == dir`, `CheckoutRoot == ""`, `OutsideWorktree == false`.
- repo root → `BindRoot == repo`, `CheckoutRoot == repo`.
- repo subdirectory (create `repo/internal/foo`) → `BindRoot == repo`.
- nested non-bare worktree (`git worktree add <repo>/wt`) → `BindRoot == repo`.
- outside non-bare worktree (`git worktree add <tmp>/wt`) → `BindRoot == wtDir`, `OutsideWorktree == true`, `CheckoutRoot == repo`.
- bare layout:
  ```go
  origin := testutil.InitRepo(t)
  root := t.TempDir()
  testutil.RunGit(t, root, "clone", "--bare", origin, ".git")
  testutil.RunGit(t, filepath.Join(root, ".git"), "worktree", "add", filepath.Join(root, "main"))
  ```
  - `ResolveWorkspaceLayout(filepath.Join(root, "main"))` → `BindRoot == root`.
  - `ResolveWorkspaceLayout(filepath.Join(root, "main", "internal"))` (create the dir) → `BindRoot == root`.
  - `ResolveWorkspaceLayout(root)` → `BindRoot == root`, `OutsideWorktree == false`.
- relative-path gitdir: within the bare layout, set `testutil.RunGit(t, filepath.Join(root, ".git"), "config", "worktree.useRelativePaths", "true")` before `worktree add`, then assert the `main` worktree still resolves to `root`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestResolveWorkspaceLayout -v`
Expected: FAIL — `ResolveWorkspaceLayout` undefined.

- [ ] **Step 3: Implement** — `internal/git/workspace.go`

```go
package git

// imports: os, path/filepath, strings

// findWorktreeRoot returns the topmost directory at or above cwd that contains a
// .git entry (a directory for a normal checkout, a file for a linked worktree or
// a bare layout's worktree), or "" when none is found.
func findWorktreeRoot(cwd string) string

// within reports whether child is parent or under it.
func within(parent, child string) bool
```
`ResolveWorkspaceLayout`:
1. `worktreeRoot := findWorktreeRoot(cwd)`; if `""` → `WorkSpaceLayout`/`WorkspaceLayout{BindRoot: cwd}`.
2. `commonGitDir := resolveGitDir(worktreeRoot)`; if `""` → `WorkspaceLayout{BindRoot: cwd}`.
3. `checkoutRoot := filepath.Dir(commonGitDir)`; if `filepath.Base(commonGitDir) != ".git"` then `checkoutRoot = commonGitDir`.
4. If `within(checkoutRoot, worktreeRoot)` → `WorkspaceLayout{BindRoot: checkoutRoot, CheckoutRoot: checkoutRoot}`.
5. Else → `WorkspaceLayout{BindRoot: cwd, CheckoutRoot: checkoutRoot, OutsideWorktree: true}`.

`findWorktreeRoot`: loop from `cwd`, `os.Lstat(filepath.Join(dir, ".git"))`; return `dir` on success, else step to `filepath.Dir(dir)`, stop when parent == dir → `""`.
`within`: `filepath.Rel(parent, child)`; true when `rel == "."` or (`rel != ".."` and not prefixed by `".."+string(filepath.Separator)`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestResolveWorkspaceLayout -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/workspace.go internal/git/workspace_test.go
git commit -m "feat(git): resolve workspace bind layout for worktrees"
```

---

### Task 2: Bind the resolved root and derive the guest CWD in the VM package

**Files:**
- Modify: `internal/sandbox/vm/run_orchestrate.go` (`PrepareSandbox` :83, `Session` :27-57, `resolveWorkspaceDir` :207)
- Modify: `internal/sandbox/vm/worktree.go` (`resolveTargetNoBranch` :19, `ResolveTarget` :82)
- Modify: `internal/sandbox/vm/reconfig.go` (`setUpSandbox` :65, calls at :104, :119)
- Modify: `internal/sandbox/vm/*_test.go` call sites of `setUpSandbox` / `ResolveTarget`
- Test: `internal/sandbox/vm/workspace_guest_test.go` (new)

**Interfaces:**
- Consumes: `git.ResolveWorkspaceLayout(cwd)` (Task 1); `mounts.WorkspaceMountPath`.
- Produces:
  ```go
  // run_orchestrate.go
  func (s *Session) GuestCWD() string   // new accessor; GuestCWD == "/workspace"+rel(bindRoot,cwd)
  // worktree.go — new trailing defaultDir parameter
  func ResolveTarget(ctx context.Context, a agent.Agent, sb msb.Sandbox, spec options.WorktreeSpec, defaultDir string, ui termio.UI) (string, error)
  // reconfig.go — new trailing guestCWD parameter
  func setUpSandbox(ctx context.Context, sb msb.Sandbox, opts options.RunOptions, cfs *reprovision.ConfigFiles, ui termio.UI, restart bool, boot vmBoot, guestCWD string) (string, error)
  ```
  `resolveTargetNoBranch` is **removed**; both no-worktree paths return `defaultDir` directly.

- [ ] **Step 1: Write the failing test** — `internal/sandbox/vm/workspace_guest_test.go`

- Table test for the helper (add it as an exported-in-package func named below):
  ```go
  func guestWorkspacePath(bindRoot, cwd string) string
  ```
  Assert: `("/workspace", "/workspace")`→`/workspace`; `("/repo","/repo/internal/foo")`→`/workspace/internal/foo`; `("/root/wt","/root/wt")`→`/workspace`; a `filepath.Rel` error case (e.g. bindRoot with a `\x00`) → `/workspace`.
- `TestSessionGuestCWD`: construct `&Session{guestCWD: "/workspace/sub"}` and assert `GuestCWD()` returns it.
- `TestSetUpSandboxReturnsGuestCWD`: mirror `setUpSandboxProvisionsConfig` (`setupsandbox_test.go:154`) exactly — `MockSandbox` + `SetDaemonShellFunc` health stub + `reprovision.LoadConfigFilesForHost` + `options.RunOptions{}` — but call `setUpSandbox(..., restart=false, boot=vmBootStarted, guestCWD="/workspace/sub")` and assert the returned target == `/workspace/sub` (the existing test asserts `defaultTargetDir`, so passing a non-default `guestCWD` distinguishes the new path).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/vm/ -run 'GuestCWD|guestWorkspacePath|SetUpSandboxReturnsGuestCWD' -v`
Expected: FAIL — undefined symbols / wrong arity.

- [ ] **Step 3: Implement**

`run_orchestrate.go`:
- Add `guestCWD string` to `Session`; add `func (s *Session) GuestCWD() string { return s.guestCWD }`.
- In `PrepareSandbox`, after `cwd, err := resolveWorkspaceDir()`:
  ```go
  layout := git.ResolveWorkspaceLayout(cwd)
  if layout.OutsideWorktree {
      ui.Warnf("worktree %q is outside its checkout %q; git may not resolve inside the VM — keep worktrees inside the checkout", cwd, layout.CheckoutRoot)
  }
  guestCWD := guestWorkspacePath(layout.BindRoot, cwd)
  ```
- Pass `layout.BindRoot` (not `cwd`) to `ensureProjectVM`; pass `guestCWD` to `setUpSandbox`; for the `--dry-run-vm` branch set `sandboxTarget = guestCWD`; set `guestCWD: guestCWD` on the returned `Session`.
- Add `guestWorkspacePath(bindRoot, cwd string) string`: `rel, err := filepath.Rel(bindRoot, cwd)`; on error or `rel == "."` return `mounts.WorkspaceMountPath`; else `path.Join(mounts.WorkspaceMountPath, filepath.ToSlash(rel))`.

`worktree.go`:
- Delete `resolveTargetNoBranch`; `ResolveTarget` gains `defaultDir string` positioned immediately before `ui`; both `return resolveTargetNoBranch(), nil` become `return defaultDir, nil`.
- Add/keep a doc comment noting `defaultDir` is the workspace bind root the session started in.

`reconfig.go`:
- `setUpSandbox` gains trailing `guestCWD string`; both `ResolveTarget(...)` calls pass `guestCWD` before `ui`.

Batch-update every existing call site (mechanical, same-shape edit): every `setUpSandbox(...)` and `ResolveTarget(...)` in `internal/sandbox/vm/*_test.go` gets the new argument `mounts.WorkspaceMountPath` (import `mounts` where needed). Search with `rg -n 'setUpSandbox\(|ResolveTarget\(' internal/sandbox/vm`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/sandbox/vm/ -v`
Expected: PASS (all pre-existing VM tests plus the new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/vm
git commit -m "feat(vm): bind enclosing checkout and start agent in the launched worktree"
```

---

### Task 3: Apply the guest CWD on attach in the session package

**Files:**
- Modify: `internal/sandbox/session/run.go` (`preparedSandbox` :23, `Run` :119, `Shell` :196, `runAttach` :229)
- Modify: `internal/sandbox/msb/testmock.go` (`MockSandbox.AttachWith` :517, add `AttachCwd` field)
- Test: `internal/sandbox/session/run_test.go` (`fakePrepared` :155, `TestRunAttach*` :111/:133)

**Interfaces:**
- Consumes: `Session.GuestCWD()` (Task 2).
- Produces:
  ```go
  type preparedSandbox interface {
      Cleanup(); Sandbox() msb.Sandbox; Target() string; ServeHostPort() int; GuestCWD() string
  }
  func runAttach(ctx context.Context, sb msb.Sandbox, projectSlug, guestCWD string, ui termio.UI, opts options.RunOptions, bashArgs ...string) error
  ```
  Attach always goes through `sb.AttachWith(ctx, "/bin/bash", bashArgs, opts...)`; the option list is `WithAttachCwd(guestCWD)` plus `WithAttachUser(sandbox.RootUser)` when `opts.Root`.

- [ ] **Step 1: Write the failing test** — `internal/sandbox/session/run_test.go`

- Give `fakePrepared` a `guestCWD string` field and `func (f *fakePrepared) GuestCWD() string { return f.guestCWD }`.
- Add `AttachCwd string` to `msb.MockSandbox` and capture `cfg.Cwd` in its `AttachWith` (alongside the existing `cfg.User`).
- Update `TestRunAttachUsesAttachWithForRoot` and `TestRunAttachDefaultDoesNotUseRoot` calls to the new `runAttach` arity, passing `guestCWD = "/workspace/sub"`.
- New assertions: in both tests assert `sb.AttachCwd == "/workspace/sub"`; keep the existing root/non-root `AttachUser` assertions.
- New `TestShellUsesGuestCWD`: `prepareSandbox` seam returns `&fakePrepared{guestCWD: "/workspace/sub"}`; run `Shell`; assert the captured `sb.AttachCwd == "/workspace/sub"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/session/ -run 'Attach|ShellUsesGuestCWD' -v`
Expected: FAIL — arity/interface mismatch.

- [ ] **Step 3: Implement**

`run.go`:
- Add `GuestCWD() string` to the `preparedSandbox` interface.
- `Run`: `runAttach(ctx, sb, projectSlug, ses.GuestCWD(), ui, opts, "-l", "-c", setup)`.
- `Shell`: `runAttach(ctx, sb, projectSlug, ses.GuestCWD(), ui, opts, "-l")`.
- `runAttach`: new `guestCWD string` param after `projectSlug`; replace the `opts.Root` branch/`Attach` call with a single:
  ```go
  attachOpts := []msbSdk.AttachOption{msbSdk.WithAttachCwd(guestCWD)}
  if opts.Root { attachOpts = append(attachOpts, msbSdk.WithAttachUser(sandbox.RootUser)) }
  exitCode, attachErr = sb.AttachWith(ctx, "/bin/bash", bashArgs, attachOpts...)
  ```
  Update the doc comment to note the CWD is applied for every agent (pi/claude-code ignore the attach target).

`testmock.go`: add `AttachCwd string`; in `AttachWith` set `m.AttachCwd = cfg.Cwd` inside the existing `len(opts) > 0` block.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/sandbox/session/ -v && go test ./internal/sandbox/msb/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/session internal/sandbox/msb/testmock.go
git commit -m "feat(session): attach agents in the launched worktree directory"
```

---

### Task 4: Changelog and docs

**Files:**
- Modify: `CHANGELOG.md` (`## [Unreleased]` → `### Added`)
- Present already: `docs/recipes/bare-worktrees.md`, `.agents/skills/bare-worktrees/SKILL.md`

**Interfaces:** none.

- [ ] **Step 1: Add the changelog line**

Under `## [Unreleased]` → `### Added`, add one entry: sessions started inside a git worktree now bind the enclosing checkout at `/workspace` and start the agent in that worktree, so bare checkouts whose worktrees live inside the checkout work host-and-VM-consistently; a worktree outside its checkout keeps the current behavior and warns. Keep it to one sentence, matching the existing bullet style.

- [ ] **Step 2: Verify docs**

Run: `bash ci/check-docs.sh .`
Expected: `docs: all internal links are valid`.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: note the worktree-aware workspace bind"
```

---

## Self-Review

- **Spec coverage:** bind rule + subdirectory rule → Task 2 (Task 1 resolves it); guest CWD → Tasks 2/3; outside-worktree warn (no own VM) → Tasks 1/2; identity unchanged → explicit Global Constraint (no task touches it); fresh-VM-only → Global Constraint (no reconfig change); recipe + skill → Task 4 (files already written); acceptance/verification → Task 4 docs check + per-task tests.
- **Step scan:** each step names files, signatures, and exact values; bodies appear only where the algorithm is not determined (the resolver and `guestWorkspacePath`).
- **Type consistency:** `ResolveWorkspaceLayout` returns `WorkspaceLayout` (Task 1) consumed in Task 2; `Session.GuestCWD()` (Task 2) consumed in Task 3; `defaultDir`/`guestCWD` names are consistent across Tasks 2–3.
- **Review Focus:** each line is pinned to a task test (Task 1 relative-path gitdir + bare root; Task 2 deep subdirectory via `guestWorkspacePath`; fresh-VM bind source is a documented constraint with no reconfig code; Task 3 non-root attach CWD).
- **Proportion:** the plan names signatures and tests, not full bodies.
