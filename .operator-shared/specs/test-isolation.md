---
description: Contract for keeping tests off real host state - the fail-fast seam mocks installed by every test package's TestMain, the host-home redirect in WithMockConfigPaths, and the seam import-chain exceptions.
read_if: Adding a test package or a TestMain, or debugging a test that reads or writes real host state (config, home, docker, msb, doctor).
---

# Test isolation

## Contract

Tests must never read or write real host state. The tool dispatches all side-effecting work through injectable seams;
under tests each seam defaults to a **fail-fast** implementation that panics on first use, so a test that reaches real
resolution fails loudly instead of silently touching the host.

Every package that has tests installs the fail-fast defaults in `TestMain`. The normal call is
`termio.InitFailFastMocks(m)` (`internal/testutil/mocks`), which installs all four seam families:

- `configpaths.InstallFailFastConfigPaths()`
- `docker.InstallFailFastGet()`
- `doctor.InstallFailFast()`
- `msb.InstallFailFastGet()`

A test that legitimately exercises a seam opts in with `WithMockConfigPaths(t)` (or the docker/msb/doctor equivalent).

Packages in the seam import chain cannot import `internal/testutil/mocks` without a test import cycle; they install the
reachable subset directly, mirroring that body:

- `configpaths` (installs configpaths + docker; doctor and msb are unreachable and would cycle)
- `doctor` (installs all four itself)
- `runtime` (installs configpaths + docker + msb; doctor imports runtime, so `InitFailFastMocks` would cycle and doctor
  is unreachable from runtime)
- `internal/agent` and `internal/filewalk` cannot install any configpaths fail-fast: `configpaths` imports `agent`, which
  imports `filewalk`. Neither package reaches a seam in its own code; if one ever gains such a call, the seam must be
  reached through an injected dependency rather than a global.

Add a `main_test.go` with such a `TestMain` to every new package that has tests.

## Host-home redirect

`configpaths.WithMockConfigPaths(t)` roots a `mockConfigPaths` in `t.TempDir()` **and** redirects `$HOME` into
`<tmp>/home`, so a stray `os.UserHomeDir()` / `os.Getenv("HOME")` in a test resolves inside the temp tree. Tests using
it must not be `t.Parallel` (`t.Setenv` forbids it). `WithMockConfigPathsHomeError(t, err)` installs a mock whose
`UserHomeDir` fails, to exercise host-home resolution failures.

## Why (the incident this encodes)

`TestConfigAgentPrintsMergedAndHostFiles` built the host opencode dir from `os.Getenv("HOME")` and wrote
`{"model":"host"}` into the developer's real `~/.config/opencode/opencode.jsonc`. The next launcher start read that file
as the agent's merged config, found it different from the host snippet merge, and raised the daemon-restart
"agent config changed" prompt (see `reconfig-decision.md`). `go test` must not leave the host in a state a later run
reacts to. The tree-wide fail-fast defaults and the `$HOME` redirect close this class.
