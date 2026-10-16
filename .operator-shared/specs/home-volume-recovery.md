---
description: Contract for recovering a project's home volume when its state.yaml is lost — reuse the live VM's mounted volume instead of minting a duplicate and orphaning the original.
read_if: Changing home-volume resolution (volume.Manager.ResolveHomeVolume / recoverOrCreateHome), liveHomeVolume in the vm package, or diagnosing a duplicate/orphaned home volume after state loss.
---

# Home-volume recovery on lost state

A project's `state.yaml` is the record of which home volume its VM mounts. It can go missing without the VM or the
volume disappearing: deleted by hand, a partial/corrupt write, or a crash between volume creation and the state write
(`EnsureNewHome` creates the volume, then writes state). Previously `ResolveHomeVolume` treated absent state as
"create fresh" and `EnsureNewHome` always minted a new timestamped `HomeVolumeName`, so the next start created a
duplicate volume, orphaned the previous one (only reclaimable by a manual `volume remove`/`prune`), and forced a
rebuild via `homeVolumeChange` (#219).

## Invariant: reuse a surviving volume, never silently duplicate

Home-volume resolution reuses an existing volume when one can be found, and only creates a fresh prefill when it
cannot. Resolution order (`volume.Manager.ResolveHomeVolume`):

1. **State** — if `state.yaml` names a volume that still resolves, reuse it (unchanged).
2. **Live VM** — when state is absent or its recorded volume is gone, adopt the home volume the project VM still
   mounts, if that volume still exists. The mount is read from the VM config as
   `cfg.Volumes[mounts.VMHomeDir].Named`; the vm package supplies it via `liveHomeVolume`
   (`client.GetSandbox(projectVMName(k))` + `handle.Config()`), because the volume package cannot import vm.
3. **Fresh prefill** — otherwise `EnsureNewHome` creates a new volume, as before.

The live VM config is authoritative for which volume is *current*, unlike probing the volume list: a project can
legitimately own several home volumes (migrate/reset keep the old one), so a name-prefix match is ambiguous. Adopting
the VM's mount is therefore preferred over any list-based probe.

Recovery rewrites `state.yaml` to the recovered volume (with the current image digest) and emits a warning naming the
recovered volume.

## Not recovered: fingerprints (rebuild still occurs)

Recovery restores the *volume reference*, not the env/secret/network/mount fingerprints, which lived only in the lost
state. The recovered state is therefore fingerprint-less and the next run still rebuilds the VM (the same on-disk
condition as the concurrent state-write race in `specs/reconfig-decision.md`). The rebuild mounts the same recovered
volume, so no user data is lost and no volume is orphaned — only the spurious rebuild remains, tracked by #183.

## Non-interactive

Recovery is silent (a warning, no prompt) and unconditional whenever a mount is recoverable, including under
`--dry-run-vm`. It introduces no new deletion: the original volume is never removed, only re-referenced.
