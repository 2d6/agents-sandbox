# Legacy Home-Volume Migration — PR #142 Review Fixes (round 2)

**Goal:** Close the second Greptile review on PR #142 and the follow-up design
discussion. All four findings reduce to one invariant — **the home resolver must
never adopt a private volume that is not a finished copy or a deliberate keep** —
plus a new user-decision path for the one state where the tool cannot tell.

**Spec:** `.operator-shared/specs/runtime-home-decoupling.md`.
**Predecessor plan:** `2026-10-15-runtime-home-migration-state.md`.
**Status:** implemented on `opencode/runtime-home-decoupling`.

## The invariant

> A private volume under a migration-target name may be handed to the home
> resolver only when it is (a) a finished copy (`copied`), (b) a deliberate keep
> (`kept`), or (c) present under a terminal `completed` migration. Any other
> present destination under a target name is an unfinished partial and must be
> removed before the resolver can run.

## Design decisions

- **Split the transient set.** `excluded` becomes `kept` (user chose to leave the
  present private volume untouched) plus `excluded` (copy failed; destination
  confirmed removed). `copied` is fully migrated. Necessity and "unclear" both
  subtract the union.
- **Ambiguous state.** A legacy-named private volume observed while `pending` can
  only mean the registry was lost, and its contents cannot be trusted. The offer
  surfaces it (prompt notice + count) with options **Copy the missing volumes;
  dismiss the existing private volumes from the migration** (records them `kept`,
  left untouched, terminal `completed`) and **Dismiss** (removes them, terminal
  `dismissed`), plus **Quit** (cleanup manually). The prompt instructs the user to
  `agents-sandbox volume remove --force <name>` the copies they want re-migrated.
- **Label rule.** "Start fresh" only when `pending` and no unclear volume exists;
  otherwise "Dismiss".
- **No unusable partial.** Automated exclusions require confirmed destination
  removal or the campaign aborts; Dismiss removes present targets not in
  `copied`/`kept` before dismissing; a campaign reaches `completed` only with no
  partial left. Finalization warns about `excluded` (uncopied) volumes.
- **`volume remove`.** New command; no prompt; refuses when a project `state.yaml`
  references the volume (naming the projects); `--force` overrides. Reference
  scan is fail-closed.
- **Listing failure.** Best-effort (warn+skip) in `pending`; a `copying` campaign
  explains the situation and blocks (fix manually and run again).

## Tasks (all implemented)

1. `state.Migration.Kept`; `accountedSet`/`unclearVolumes`; copying necessity
   subtracts `Copied ∪ Kept ∪ Excluded`.
2. `CopyVolumes` seeds `Kept` from present targets on the `pending → copying`
   transition; `finalizeMigration` warns `Excluded`.
3. `Offer` ambiguous-state prompt/choices/wording; `handleOfferListingError`
   (block+guidance for `copying`, warn+skip for `pending`); `dismissMigration`
   removes unclear (preserving `copied`/`kept`).
4. `state.HomeVolumeReferences` + `volume.RemoveVolumes` + `volume remove` CLI.
5. Coverage (new code ~100%), `make check`, docs, CHANGELOG.
