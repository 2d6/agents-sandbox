# Custom-base decoupling (tool base) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the node/agent/docker tool stages build on a tool-owned `agents-sandbox-toolbase` (`debian:trixie-slim` + `curl`/`ca-certificates`/`tar`) instead of the user's custom base, so a custom base no longer needs `curl`/`tar` and the tool layers cache across bases.

**Architecture:** `RenderDockerfile` gains one more reserved stage, `agents-sandbox-toolbase`, emitted after the earlier user stages and before the tool stages. `nodeStage`, `agentStage`, and `dockerStage` start from `agents-sandbox-toolbase` (the agent stage from the node stage when node is required), and install unconditionally — the fixed tool base never carries a tool, so the old `command -v` skip checks are dead and removed. The runner stage still builds on `agents-sandbox-base` (the managed block or the custom base) and merges the tool trees with `COPY`; the adoption steps record provenance as before. The docker runtime-prerequisite check drops `curl`/`tar`, which are needed only by the download now living in the tool base.

**Tech Stack:** Go 1.26, `internal/sandbox/image`; Docker (integration tests only).

**Spec:** [.operator-shared/specs/image-composition.md](../specs/image-composition.md) (stage diagram, "Tool artifact contract", "User-provided detection and provenance", "Docker marker").

Tracking issue: [inoio/agents-sandbox#143](https://github.com/inoio/agents-sandbox/issues/143).

## Global Constraints

- Stage aliases are deterministic and reserved (`agents-sandbox-base|toolbase|node|agent|docker|runner`); a project Dockerfile declaring one is a hard error. Never uuid/hash-suffix an alias.
- `dockerfile-id` stays a content hash of the rendered Dockerfile + the profile.d merge script + the resolved agent version; the tool-base stage is part of the rendered content, so it is folded in automatically.
- Each tool states its pinned version as an `ARG` default in the rendered content (node, agent, docker); an empty agent version renders the ARG without a default.
- `baseImageRef`/`resolveBaseDigest` resolve the `agents-sandbox-base` stage's underlying image (the embedded `debian:trixie-slim` for managed, the user's image for custom); the tool-base stage never becomes the image's base label.
- `BuildOptions.Force`, `NoCache`, and the `dockerfile-id` skip-build contract are unchanged.
- Tool artifact contract: every tool lands under `/opt/agents-sandbox`; binaries are exposed through `/opt/agents-sandbox/bin`, appended to `PATH`.
- Target platforms: Linux (KVM) and macOS (Apple Silicon).
- Do not add any inline comments beyond those already idiomatic in these files.

## Review Focus

- **Custom base with no `curl`/`tar`** (the #143 report): a project `FROM debian:trixie-slim` with an empty body must build — the agent install now runs on the tool base, not the custom base. Covered by the new integration case in Task 3.
- **pi's npm install now runs on the slim tool base**, not the managed tools block: if pi's npm package needs a tool (git, python, …) the managed block used to provide, the build breaks. Covered by the existing pi integration tests (`TestRenderDockerfileNodeAgentNodeRuns`, `TestRenderDockerfileBuilds`); if they fail, widen the tool-base package list.
- **A base that already provides the agent/node/dockerd** is still recorded as `user` provenance: the tool stage now always downloads into `/opt/agents-sandbox`, and the runner's `PATH` (base entries first) plus the adoption step must still mark it `user`. Covered by the unchanged adoption assertions in Task 1.
- **Docker on a custom base without `curl`/`tar`**: the runtime prerequisite check must no longer reject it now that the download moved to the tool base. Covered by Task 2 + the docker no-curl integration case in Task 3.
- **Managed base** still composes, and the shared `debian:trixie-slim` layer serves both the managed block and the tool base (no second pull). Covered by Task 1's managed-composition assertions + Task 3.

---

### Task 1: Emit the tool base and rewire the tool stages

**Files:**
- Modify: `internal/sandbox/image/dockerfile.go`
- Test: `internal/sandbox/image/render_test.go`

**Interfaces:**
- Produces: `toolbaseStageAlias = "agents-sandbox-toolbase"`; `toolbaseStage() string`; `nodeStage() string`, `agentStage(a agent.Agent, parentAlias, agentVersion string) string`, `dockerStage() string` (base alias parameters dropped).
- Consumes: existing `baseStageAlias`, `nodeStageAlias`, `agentStageAlias`, `dockerStageAlias`, `runnerStageAlias`, `toolDir`, `toolBin`, `nodeVersion`, `dockerVersion`.

- [ ] **Step 1: Update the existing tests to the new composition**

In `internal/sandbox/image/render_test.go`:

- `TestRenderDockerfileManagedComposition` (line ~60): replace `"FROM agents-sandbox-base AS agents-sandbox-agent"` with `"FROM agents-sandbox-toolbase AS agents-sandbox-agent"` and add `"FROM debian:trixie-slim AS agents-sandbox-toolbase"` to the `want` slice.
- `TestRenderDockerfileOmitsNodeStageForNodeFreeAgent` (line ~97): replace `"FROM agents-sandbox-base AS "+agentStageAlias` with `"FROM agents-sandbox-toolbase AS "+agentStageAlias`.
- `TestRenderDockerfileEmitsNodeStageForPi` (line ~115): replace `"FROM agents-sandbox-base AS "+nodeStageAlias` with `"FROM agents-sandbox-toolbase AS "+nodeStageAlias`.
- `TestRenderDockerfileStageOrder` (line ~231): replace the `order` slice with the rendered order:

```go
	order := []string{
		"FROM debian:trixie-slim AS " + toolbaseStageAlias,
		"FROM " + toolbaseStageAlias + " AS " + agentStageAlias,
		"FROM " + toolbaseStageAlias + " AS " + dockerStageAlias,
		"FROM debian:trixie-slim AS " + baseStageAlias,
		"FROM " + baseStageAlias + " AS " + runnerStageAlias,
	}
```

- `TestRenderDockerfileDockerStage` (line ~257): replace `"FROM agents-sandbox-base AS agents-sandbox-docker"` with `"FROM agents-sandbox-toolbase AS agents-sandbox-docker"`.
- `TestRenderDockerfileCustomBaseReusesAlias` (line ~461): replace `"FROM final AS agents-sandbox-agent"` with `"FROM agents-sandbox-toolbase AS agents-sandbox-agent"`.
- `TestRenderDockerfileReservedAliasCollision` (line ~560): add `toolbaseStageAlias` to the alias slice.

Add these new tests:

```go
// TestRenderDockerfileToolStagesUseToolbase guards #143: the tool stages build
// on the tool-owned tool base, not on the user's custom base, so a custom base
// needs neither curl nor tar.
func TestRenderDockerfileToolStagesUseToolbase(t *testing.T) {
	a, _ := agent.Lookup("opencode")
	project := []byte("FROM ubuntu:24.04\nRUN echo custom\n")
	s := mustRender(t, a, project, true)
	for _, want := range []string{
		"FROM debian:trixie-slim AS " + toolbaseStageAlias,
		"apt-get install -y --no-install-recommends ca-certificates curl tar",
		"FROM " + toolbaseStageAlias + " AS " + agentStageAlias,
		"FROM " + toolbaseStageAlias + " AS " + dockerStageAlias,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered Dockerfile missing %q; got:\n%s", want, s)
		}
	}
	if strings.Contains(s, "FROM ubuntu:24.04 AS "+agentStageAlias) ||
		strings.Contains(s, "FROM ubuntu:24.04 AS "+dockerStageAlias) {
		t.Errorf("tool stages must not build on the custom base; got:\n%s", s)
	}
}

// TestRenderDockerfileToolStagesInstallUnconditionally guards that the tool
// stages no longer probe the base for the tool: they build on the fixed tool
// base, which never carries it, so the install runs unconditionally.
func TestRenderDockerfileToolStagesInstallUnconditionally(t *testing.T) {
	a, ok := agent.Lookup("pi")
	if !ok {
		t.Fatal("pi agent not registered")
	}
	s := mustRender(t, a, nil, true)
	for _, stage := range []string{nodeStageAlias, agentStageAlias, dockerStageAlias} {
		block := stageBlock(t, s, stage)
		if strings.Contains(block, "command -v") {
			t.Errorf("tool stage %q must not probe the base for the tool; got:\n%s", stage, block)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/image/ -run 'Composition|NodeStage|StageOrder|DockerStage|CustomBaseReusesAlias|ReservedAlias|ToolStages' -v`
Expected: FAIL — `toolbaseStageAlias` undefined (compile error); stage assertions still see the old parents.

- [ ] **Step 3: Implement in `internal/sandbox/image/dockerfile.go`**

Add the alias constant and register it (the `const` block at line ~30 and `reservedStageAliases` at line ~39):

```go
	toolbaseStageAlias = "agents-sandbox-toolbase"
```

```go
var reservedStageAliases = []string{
	baseStageAlias, toolbaseStageAlias, nodeStageAlias, agentStageAlias, dockerStageAlias, runnerStageAlias,
}
```

Add `toolbaseStage` (place it near `nodeStage`):

```go
// toolbaseStage renders the tool-owned stage the node/agent/docker stages build
// on. It carries only what the tool downloads need (curl, ca-certificates, tar)
// and is never part of the runner's lineage, so a custom base needs neither
// binary and the tool layers cache across bases. It shares the debian:trixie-slim
// layer with the managed base block.
func toolbaseStage() string {
	return fmt.Sprintf(`FROM debian:trixie-slim AS %s
USER root
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl tar && \
    rm -rf /var/lib/apt/lists/*
`, toolbaseStageAlias)
}
```

Change `nodeStage` to start from the tool base and drop the `command -v` probe — `FROM %s AS %s` uses `toolbaseStageAlias`, and the `RUN` becomes a `mkdir` followed by the arch `case` and the download (no `if command -v node`):

```go
func nodeStage() string {
	return fmt.Sprintf(`FROM %s AS %s
USER root
ARG NODE_VERSION=%s
RUN mkdir -p %s && \
    case "$(uname -m)" in \
      x86_64) NODE_ARCH=x64 ;; \
      aarch64) NODE_ARCH=arm64 ;; \
      *) echo "error: unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://nodejs.org/dist/${NODE_VERSION}/node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz" \
      | tar -xz -C %s --strip-components=1
ENV PATH="%s:${PATH}"
`, toolbaseStageAlias, nodeStageAlias, nodeVersion, toolDir, toolDir, toolBin)
}
```

Change `agentStage`'s `RUN` to install unconditionally (keep the `mkdir -p toolBin` and the version `ARG`; drop the `if command -v ... then : else ... fi` wrapper):

```go
		RUN mkdir -p %s && %s
```

Change `dockerStage` to start from the tool base and drop the probe:

```go
func dockerStage() string {
	return fmt.Sprintf(`FROM %s AS %s
USER root
ARG DOCKER_VERSION=%s
RUN set -e; mkdir -p %s && \
    curl -fsSL "https://download.docker.com/linux/static/stable/$(uname -m)/docker-${DOCKER_VERSION}.tgz" \
      | tar -xz -C %s --strip-components=1
`, toolbaseStageAlias, dockerStageAlias, dockerVersion, toolBin, toolBin)
}
```

Rewire `RenderDockerfile` (line ~201): emit the tool base after the earlier stages, point the agent stage at it, and place the base stage after the tool stages:

```go
	blocks := make([]string, 0, 7)
	blocks = append(blocks, earlier, toolbaseStage())
	agentParent := toolbaseStageAlias
	if a.ImageSpec().RequiresNode {
		blocks = append(blocks, nodeStage())
		agentParent = nodeStageAlias
	}
	blocks = append(blocks, agentStage(a, agentParent, agentVersion))
	if docker {
		blocks = append(blocks, dockerStage())
	}
	blocks = append(blocks, baseStage, runnerStage(a, baseAlias, body, docker, dockerInjected))
	return []byte(joinBlocks(blocks...)), nil
```

Update the doc comment above `nodeStage`/`dockerStage`/`RenderDockerfile` to state the tool base is the parent (remove the "only when the base does not already provide it" wording from the install stages; detection is the adoption step's job).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/image/ ./internal/agent/`
Expected: PASS. Also run `golangci-lint run ./internal/sandbox/image/...` and fix any reported issue.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/image/dockerfile.go internal/sandbox/image/render_test.go
git commit -m "feat(image): build tool stages on a tool-owned base (#143)"
```

---

### Task 2: Drop curl/tar from the docker runtime prerequisite check

**Files:**
- Modify: `internal/sandbox/image/dockerfile.go` (`dockerAdoptionBlock`)
- Test: `internal/sandbox/image/render_test.go`

**Interfaces:**
- Consumes: Task 1 (the docker download now lives in the tool base).
- Produces: `dockerAdoptionBlock()` checks `iptables git ps xz` only.

- [ ] **Step 1: Write the failing test**

Add to `internal/sandbox/image/render_test.go`:

```go
// TestRenderDockerfileDockerPrereqsExcludeDownloadTools guards #143: the docker
// runtime prerequisite check must not require curl or tar, which are needed only
// by the download (now in the tool base), not at runtime.
func TestRenderDockerfileDockerPrereqsExcludeDownloadTools(t *testing.T) {
	a, _ := agent.Lookup("opencode")
	final := finalStage(t, mustRender(t, a, nil, true))
	if !strings.Contains(final, "iptables git ps xz") {
		t.Errorf("the docker prerequisite check must keep the runtime prerequisites; got:\n%s", final)
	}
	if strings.Contains(final, "iptables git ps xz curl tar") {
		t.Errorf("the docker prerequisite check must not require curl/tar; got:\n%s", final)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/image/ -run DockerPrereqsExclude -v`
Expected: FAIL — the loop still lists `curl tar`.

- [ ] **Step 3: Implement**

In `dockerAdoptionBlock`, change the `for p in iptables git ps xz curl tar; do` loop to `for p in iptables git ps xz; do`, and update the comment to note that the download's `curl`/`tar` are no longer required because the docker install runs on the tool base.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sandbox/image/ -run 'DockerPrereqs' -v`
Expected: PASS (including the existing `TestRenderDockerfileDockerPrereqsCheckedInFinalStage`).

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/image/dockerfile.go internal/sandbox/image/render_test.go
git commit -m "fix(image): drop curl/tar from the docker runtime prerequisite check (#143)"
```

---

### Task 3: Integration coverage for a custom base without curl/tar

**Files:**
- Test: `internal/sandbox/image/render_integration_test.go`

**Interfaces:**
- Consumes: Task 1–2 behaviour.

- [ ] **Step 1: Add the regression cases**

In `TestRenderDockerfileBuilds`'s `cases` slice, add:

```go
		{"custom-base-slim-no-curl", []byte("FROM debian:trixie-slim\n"), false},
		{"custom-base-slim-no-curl-docker", []byte(
			"FROM debian:trixie-slim\n" +
				"RUN apt-get update && apt-get install -y --no-install-recommends iptables git procps xz-utils && rm -rf /var/lib/apt/lists/*\n"), true},
```

The first is the #143 regression: `debian:trixie-slim` ships no `curl`, so the old tool stages (starting from that base) failed the agent install; the new tool base fixes it. The second exercises the docker path on a base that never installs `curl`/`tar`.

- [ ] **Step 2: Verify compilation of tagged tests**

Run: `go test -tags integration -run xxx ./internal/sandbox/image/`
Expected: compiles (no match without the tag; the case list is only read under the tag).

- [ ] **Step 3: Run the integration suite on a host with Docker**

Run: `make test-integration` (or `go test -tags integration ./internal/sandbox/image/ -run 'TestRenderDockerfileBuilds|NodeAgentNodeRuns|ToolLayersStayCached|AgentVersionRebuildsInstall'`)
Expected: PASS. If pi's `TestRenderDockerfileNodeAgentNodeRuns` or an opencode build fails on the slim tool base, add the missing package to `toolbaseStage`'s apt list and re-run.

- [ ] **Step 4: Commit**

```bash
git add internal/sandbox/image/render_integration_test.go
git commit -m "test(image): build a custom base without curl/tar (#143)"
```

---

### Task 4: Docs, spec, changelog, catalog

**Files:**
- Modify: `docs/runner-image.md`
- Modify: `.operator-shared/specs/image-composition.md`
- Modify: `CHANGELOG.md`
- Modify: `.operator-shared/catalog.md`

- [ ] **Step 1: Update the docs**

- `docs/runner-image.md`:
  - Stage list (~line 34): add a **`agents-sandbox-toolbase`** bullet — the internal stage (`debian:trixie-slim` + `curl`/`ca-certificates`/`tar`) the tool stages build on, not part of the runner's lineage.
  - Node/agent/docker bullets (~line 38-47): drop "only when the base lacks `node`/its binary/`dockerd`"; state the base is `agents-sandbox-toolbase` (agent from the node stage when node is required); keep the artifact/install-command notes.
  - Custom-base requirements (~line 80-84): remove the "`curl` and `tar` in the base image itself" bullet; reword the Docker-only bullet to `iptables`, `git`, `ps`, `xz` (no `curl`/`tar`; the download runs on the tool base).
  - Docker example (~line 140-155): drop `curl tar` from the base-stage `RUN` and the two comments that explain the tool stages need them; keep the runtime-prereq body line and the marker example.
- `.operator-shared/specs/image-composition.md`:
  - Stage diagram: add `FROM debian:trixie-slim AS agents-sandbox-toolbase` and change the node/agent/docker parents to it (base stage after the tool stages).
  - "Tool artifact contract": note the tool stages build on the tool base and install unconditionally; detection is the adoption step's job.
  - "User-provided detection and provenance": state that the install stages no longer probe the base (they cannot see it), so a base-provided tool is recorded by adoption and keeps `PATH` precedence.
  - "Docker marker": drop the "only `curl` and `tar` remain base-image requirements" sentence; note the docker download runs on the tool base.
  - "Invariants": add `toolbase` to the reserved-alias set; note `baseImageRef` still resolves the `agents-sandbox-base` stage, never the tool base.
- `CHANGELOG.md` `[Unreleased]` `Changed`: "Runner image: the node/agent/docker install stages now build on a tool-owned `agents-sandbox-toolbase` (`debian:trixie-slim` + `curl`/`ca-certificates`/`tar`) instead of the user's base, so a custom base no longer needs `curl`/`tar` inside the image and the tool layers cache across bases (fixes #143)."
- `.operator-shared/catalog.md`: extend the `image-composition.md` entry's Description to mention the tool base; add this plan under the `plans/` tree.

- [ ] **Step 2: Verify docs link checks**

Run: `make check` (or `ci/check-docs.sh`)
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add docs/runner-image.md .operator-shared/specs/image-composition.md CHANGELOG.md .operator-shared/catalog.md
git commit -m "docs(image): document the tool base and dropped curl/tar requirement (#143)"
```

---

## Verification

- [ ] `make check` passes (fmt, lint, test).
- [ ] `make compile-all` passes (integration-tagged files compile).
- [ ] `make test-integration` passes on a Docker host.
- [ ] Finalization per `AGENTS.md` (ask the user; `/workspace` is a bind mount).
