---
description: Contract for the per-startup project-VM reconfiguration decision: how changed settings are detected via persisted fingerprints, which changes recreate the VM vs restart the daemon, the fingerprint-preservation invariant, and the known state-write race.
read_if: Changing decideReconfig/PlanReconfig, the env/secret/network/mount fingerprints, the reconfig prompt, or diagnosing a spurious VM rebuild or "network policy changed" prompt.
---

# Project-VM reconfiguration decision

`PrepareSandbox` calls `decideReconfig` (`internal/sandbox/vm/reconfig.go`) once per startup to choose between recreating
the project VM, restarting the agent daemon, or doing nothing. The decision is computed by `reconfig.PlanReconfig` and
applied by `reconfig.ResolveReconfig` (silent when alone, a keep/quit[/restart] prompt when other clients are attached).

## Fingerprint-backed change detection

The microsandbox SDK does not round-trip env, secrets, network, or mounts when reading an existing VM back, so those
changes are detected against fingerprints persisted in the per-`(slug, agent)` `state.yaml` (`state.HomeState`):

- `EnvState`, `SecretState`, `MountState` — content hashes. Empty applied means "no persisted state yet" and counts as
  changed **only when the desired setting is non-empty**.
- `NetworkState` — a `network.Policy.Fingerprint()` hash. Empty applied counts as changed **unconditionally**, to force
  one-time migration of VMs created before the deny-by-default network change (#86). `BuildNetworkState` applies
  `Policy.Effective()`, so an unset profile fingerprints as `none`; `Fingerprint` sorts its lines, so list order does not
  matter.

Fingerprints are written by `persistConfigHashes`, which runs only when the VM was created this run (`boot ==
vmBootCreated`).

## Invariant: preserve fingerprints on every state rewrite

Every path that rewrites a project's `HomeState` must carry the existing env/secret/network/mount fingerprints forward,
because an absent fingerprint is treated as "changed". `volume.operations.volumeOp`, `volume.Manager.ApplyHomeAction`,
and `volume.Manager.RecordHomeImage` all preserve them. Dropping them makes the next run rebuild the VM and blame a
setting (e.g. the network policy) that did not change.

## Determinism

Change detection is order-independent by design: `state.SecretsContentHash`, `state.EnvContentHash`, and
`network.Policy.Fingerprint` all sort their inputs before hashing, and `reprovision.AgentConfigEqual` compares the merged
and mirror files by map lookup. The helper outputs feeding them are also kept deterministic: `reprovision.ConfigFiles.Keys`
has a single ordering authority (the merged config leads when snippets exist, the remainder sorted; callers must not
re-sort) and `envsecret.BuildSecretsFromSpecs` emits entries in sorted `EnvVar` order. Map iteration order therefore never
reaches the decision — a periodic spurious rebuild is the state-write race below, not input ordering.

## Known residual race

State resolution (`volume.Manager.EnsureNewHome`, which writes a fingerprint-less fresh `HomeState`) and
`persistConfigHashes` run **outside** the `ensure-vm.lock` flock that serializes VM creation (`ensureProjectVM`). A
second session starting concurrently can read the fresh, fingerprint-less state after the first session created the VM
but before it persisted fingerprints, and then see a spurious "network policy" (and env/secret/mount) change. That
surfaces as a silent rebuild when alone or a rebuild/restart prompt when another client is attached — despite no setting
changing. Closing it means making the fingerprint write part of the same critical section as VM creation, or writing the
fingerprints together with the fresh home volume. Tracked as issue #183.
