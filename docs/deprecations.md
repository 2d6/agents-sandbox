---
title: Deprecations
description: "Every deprecated agents-sandbox flag, config key, environment variable, and project-Dockerfile spelling, with its replacement and removal target."
layout: default
nav_order: 115
---
# Deprecations

agents-sandbox is pre-1.0 but holds a high UX bar: a renamed flag, config key, environment variable, or command keeps
working with a one-time warning before it is removed.

**How a deprecation behaves:**

- A renamed flag, config key, or environment variable still works with **lower precedence** than its replacement and
  prints a **one-time warning** to stderr.
- A deprecated project-Dockerfile spelling has no replacement to resolve against, so it warns on **every** ensure that
  uses the Dockerfile (see [Project Dockerfiles](#project-dockerfiles)).
- Deprecated forms are **hidden from `--help`** but still honored.
- A deprecated form survives **at least one minor release** after the release that introduces it, and its removal is
  announced in the release notes. A removal only lands in a minor release, never a patch.

The list below is the current register. It is also recorded in the `Deprecated` section of the
[changelog](https://github.com/inoio/agents-sandbox/blob/main/CHANGELOG.md).

| Deprecated form | Replacement | Eligible removal |
|---|---|---|
| `--dind` flag, `dind` config key, `AGENTS_SANDBOX_DIND` | `--docker`, `docker`, `AGENTS_SANDBOX_DOCKER` | 0.8.0 |
| `agents-sandbox/runner-base-dind` base image | `agents-sandbox/runner-base-docker` | 0.8.0 |
| `# agents-sandbox:dind` Dockerfile marker | `# agents-sandbox:docker` | 0.8.0 |
| `OPENCODE_SANDBOX_*` environment variables | `AGENTS_SANDBOX_*` | 0.8.0 |
| `sandbox list --names` | global `-q` / `--quiet` | 0.8.0 |
| `prune --age` (and `-a`) | `--older-than` | 0.8.0 |
| `volume migrate` / `reset` / `edit` `--rebuild` | run `agents-sandbox build` first | 0.8.0 |
| `--opencode-version` | `--agent-version` | 0.7.0 |

The removal column is an **eligibility floor, not a promise**: a removed form only disappears in a minor release, never a
patch.

## Project Dockerfiles

A project Dockerfile can still reference the managed docker base or place the injection marker with the legacy `dind`
spelling. agents-sandbox accepts both and warns whenever the project Dockerfile is used to ensure the runner image
(including when the cached image is reused). Prefer the `docker` spelling:

```dockerfile
FROM agents-sandbox/runner-base-docker
# agents-sandbox:docker
```

See [Runner Image]({% link runner-image.md %}) for the base references and the marker.
