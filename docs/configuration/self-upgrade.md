---
title: Self-upgrade
description: "How agents-sandbox checks for new releases and upgrades itself, and how to configure the upgrade mode and interval."
layout: default
parent: Configuration
nav_order: 80
---

# Self-upgrade

agents-sandbox checks GitHub for a newer release when you start `run`/`shell`. The check is throttled to at most once
per `upgrade.interval` (default `1d`, minimum `1h`) and is skipped entirely for local `dev` builds, for Homebrew-managed
installs (see [Homebrew Installs](#homebrew-installs)), and when a check is already within the interval. Transient
network failures are ignored so an offline start is never blocked. When a newer
release is found, the `upgrade.mode` decides what happens:

| Mode                 | Behavior                                                                                                            |
|----------------------|---------------------------------------------------------------------------------------------------------------------|
| `prompt` (default)   | Ask what to do: continue, don't ask again for this version, upgrade & continue, or upgrade & exit. Falls back to a plain notice when not interactive. |
| `notify`             | Print a notice that a newer release exists; never installs anything.                                                |
| `auto`       | Silently download and replace the binary, then continue running the current version.                                |
| `auto-exit`  | Silently download and replace the binary, then exit so the next invocation uses the new version.                    |

The `upgrade` command (`agents-sandbox upgrade`) checks for and installs the latest release at any time, independent of
`upgrade.mode`/`upgrade.interval`. Upgrading replaces the running executable with the release binary for your platform
(`agents-sandbox-<os>-<arch>` from the GitHub release assets); because a running process cannot swap its own binary, an
upgrade (or `auto-exit`) takes effect on the next invocation.

## Homebrew Installs

When agents-sandbox detects that the running binary is a Homebrew-managed keg (it lives under a `Cellar` directory), it
defers version management to Homebrew so the launcher never replaces the binary out from under `brew`:

* The automatic `run`/`shell` check is skipped entirely, regardless of `upgrade.mode`.
* The `upgrade` command prints `brew update && brew upgrade agents-sandbox` instead of downloading a release.

Update these installs with `brew update && brew upgrade agents-sandbox`.
