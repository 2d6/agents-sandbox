---
title: Deprecations
description: "Every deprecated agents-sandbox flag, config key, environment variable, and project-Dockerfile spelling, with its replacement and removal target."
layout: default
nav_order: 110
---
# Deprecations

agents-sandbox is pre-1.0 but holds a high UX bar: a renamed flag, config key, environment variable, or command keeps
working with a one-time warning before it is removed.

**How a deprecation behaves:**

- The old spelling still works with **lower precedence** than its replacement and prints a **one-time warning** to stderr.
- It is **hidden from `--help`** but still honored.
- It survives **at least one minor release** after the release that introduces it, and its removal is announced in the
  release notes. A removal only lands in a minor release, never a patch.

The list below is the current register. It is also recorded in the `Deprecated` section of the
[changelog](https://github.com/inoio/agents-sandbox/blob/main/CHANGELOG.md).

| Deprecated form | Replacement | Removed in |
|---|---|---|
| `--dind` flag, `dind` config key, `AGENTS_SANDBOX_DIND` | `--docker`, `docker`, `AGENTS_SANDBOX_DOCKER` | 0.8.0 |
| `agents-sandbox/runner-base-dind` base image | `agents-sandbox/runner-base-docker` | 0.8.0 |
| `# agents-sandbox:dind` Dockerfile marker | `# agents-sandbox:docker` | 0.8.0 |
| `OPENCODE_SANDBOX_*` environment variables | `AGENTS_SANDBOX_*` | 0.8.0 |
| `sandbox list --names` | global `-q` / `--quiet` | 0.8.0 |
| `prune --age` (and `-a`) | `--older-than` | 0.8.0 |
| `volume migrate` / `reset` / `edit` `--rebuild` | run `agents-sandbox build` first | 0.8.0 |
| `--opencode-version` | `--agent-version` | 0.7.0 |

## Project Dockerfiles

A project Dockerfile can still reference the managed docker base or place the injection marker with the legacy `dind`
spelling. agents-sandbox accepts both and warns when it composes the runner image. Prefer the `docker` spelling:

```dockerfile
FROM agents-sandbox/runner-base-docker
# agents-sandbox:docker
```

See [Runner Image]({% link runner-image.md %}) for the base references and the marker.
