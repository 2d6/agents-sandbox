package runtimemigration

import (
	"context"
	"errors"
	"fmt"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// Offer choices. "copy" copies every necessary legacy volume at once; "fresh"
// records a permanent opt-out — labelled "Start fresh" when there is nothing to
// clean up, or "Dismiss" when unclear private volumes must be removed; "quit"
// aborts the session without a decision, so the offer returns next time. There
// is deliberately no "decide later": the session cannot proceed while the
// decision is unmade, because resolving the home volume would create a fresh one
// and overwrite the project's recorded reference before the old data could be
// copied.
const (
	choiceCopy  = "c"
	choiceFresh = "s"
	choiceQuit  = "q"
)

// MigrationName is the root-state migrations registry key for the one-time
// legacy home-volume copy.
const MigrationName = "runtime_home"

// ErrMigrationPending reports that the one-time legacy home-volume decision has
// not been made. A non-interactive session (including --yes) cannot make it, so
// it returns this error before touching the home volume rather than silently
// orphaning the legacy data. An interactive session returns it only when the
// user quits the prompt or the prompt fails.
var ErrMigrationPending = errors.New("legacy home-volume migration pending")

// Offer presents the one-time, opt-in copy of legacy microsandbox home volumes
// into the private runtime home, at the start of an interactive session.
//
// The offer is global: one acceptance copies every necessary volume. It is shown
// while the migration is undismissed and either has volumes to copy or has
// present private volumes of unclear status (a previous copy whose progress was
// lost). The latter is surfaced so the user decides explicitly instead of the
// session silently adopting a possibly-incomplete home.
//
// A non-interactive session is never prompted and never copies: because
// proceeding would overwrite the project's recorded home-volume reference before
// the copy can happen, it returns ErrMigrationPending with guidance instead. A
// failure to read the persisted state or derive necessity is best-effort: it
// warns and skips rather than blocking the session. A failed copy aborts the
// session (ErrMigrationPending): continuing would orphan the old data, while the
// per-volume copy already removed any partial volume so necessity stays true and
// the offer returns next session.
func Offer(ctx context.Context, client msb.Client, ui termio.UI) error {
	rootState, err := state.ReadRootState()
	if err != nil {
		// The copy is a best-effort convenience: a corrupt or unreadable
		// launcher-global state file must not block the agent from running.
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	migration, _ := rootState.Migration(MigrationName)
	if migration.Status == state.MigrationCompleted || migration.Status == state.MigrationDismissed {
		return nil
	}

	names, err := necessaryVolumesFor(migration)
	if err != nil {
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	presentTargets, err := presentLegacyTargetsFor()
	if err != nil {
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	unclear := unclearVolumes(migration, presentTargets)

	if len(names) == 0 && len(unclear) == 0 {
		if migration.Status == state.MigrationCopying {
			return finalizeCompletedCampaign()
		}
		return nil
	}

	if !ui.IsInteractive() {
		return fmt.Errorf(
			"%w: %d legacy microsandbox home volume(s) can be copied into agents-sandbox "+
				"and %d present private volume(s) have unclear status; "+
				"run an interactive `run`/`shell` session to decide, "+
				"or use `agents-sandbox volume remove` to clean up and dismiss",
			ErrMigrationPending, len(names), len(unclear),
		)
	}

	choice, err := ui.Select(offerPrompt(len(names), len(unclear)), offerChoices(migration, len(unclear)), choiceQuit)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMigrationPending, err)
	}

	switch choice {
	case choiceCopy:
		if err := CopyVolumes(ctx, client, ui); err != nil {
			return fmt.Errorf("copying legacy home volumes: %w", err)
		}
		return nil
	case choiceFresh:
		return dismissMigration(ctx, client)
	default:
		return ErrMigrationPending
	}
}

// presentLegacyTargetsFor resolves both homes and returns the present private
// volumes under a legacy target name.
func presentLegacyTargetsFor() ([]string, error) {
	legacyHome, err := runtime.LegacyHome()
	if err != nil {
		return nil, err
	}
	return presentLegacyTargets(legacyHome, runtime.PrivateHome())
}

// withMigrationLock runs fn while holding the host-only migration lock, so a
// terminal write cannot race an in-flight copy campaign's own writes. It must
// only wrap state writes made outside CopyVolumes: CopyVolumes already holds the
// lock, and flock is not reentrant.
func withMigrationLock(fn func() error) error {
	release, err := acquireMigrationLock()
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// finalizeCompletedCampaign closes out an open campaign that has nothing left to
// copy (it crashed after copying everything but before recording completion). It
// re-reads state under the lock and only writes completed while the entry is
// still copying, so it never clobbers a concurrent terminal write.
func finalizeCompletedCampaign() error {
	return withMigrationLock(func() error {
		root, err := state.ReadRootState()
		if err != nil {
			return err
		}
		m, _ := root.Migration(MigrationName)
		if m.Status != state.MigrationCopying {
			return nil
		}
		return saveMigration(state.Migration{Status: state.MigrationCompleted})
	})
}

// dismissMigration records the permanent opt-out under the lock, re-reading first
// so a concurrent campaign's terminal write (completed or dismissed) is not
// overwritten. It first removes every unclear private volume: recording the
// dismissal while a usable partial remains would let the home resolver adopt an
// incomplete home under a permanent opt-out. Volumes that were copied or kept at
// the user's choice are preserved. A cleanup failure aborts the dismissal.
func dismissMigration(ctx context.Context, client msb.Client) error {
	return withMigrationLock(func() error {
		root, err := state.ReadRootState()
		if err != nil {
			return err
		}
		m, _ := root.Migration(MigrationName)
		if m.Status == state.MigrationCompleted || m.Status == state.MigrationDismissed {
			return nil
		}
		if err := removeUnclearDestinations(ctx, client, m); err != nil {
			return err
		}
		return saveMigration(state.Migration{Status: state.MigrationDismissed})
	})
}

// removeUnclearDestinations removes every present private volume under a legacy
// target name that is neither copied (fully migrated) nor kept (dismissed at the
// user's choice). Such a volume is an interrupted copy's partial — or, in the
// pending state, a copy whose registry entry was lost — and leaving it would let
// the home resolver adopt an incomplete home.
func removeUnclearDestinations(ctx context.Context, client msb.Client, m state.Migration) error {
	presentTargets, err := presentLegacyTargetsFor()
	if err != nil {
		return err
	}
	protected := make(map[string]bool, len(m.Copied)+len(m.Kept))
	for _, names := range [][]string{m.Copied, m.Kept} {
		for _, name := range names {
			protected[name] = true
		}
	}
	for _, name := range presentTargets {
		if protected[name] {
			continue
		}
		if err := removeVolume(ctx, client, name); err != nil {
			return err
		}
	}
	return nil
}

// offerPrompt builds the prompt text, appending the unclear-volumes notice and
// the user's cleanup options when present.
func offerPrompt(copyCount, unclearCount int) string {
	text := fmt.Sprintf("Copy %d legacy home volume(s) into agents-sandbox?", copyCount)
	if unclearCount == 0 {
		return text
	}
	return text + fmt.Sprintf(
		"\n\n%d present private volume(s) have unclear status: a previous copy whose progress was lost, "+
			"so their contents cannot be trusted as complete. Copying leaves them untouched and dismisses them "+
			"from the migration. To migrate one fully, Quit, remove its private copy with "+
			"`agents-sandbox volume remove --force <name>`, and run again; keep the copies you do not want "+
			"migrated.",
		unclearCount,
	)
}

// offerChoices builds the menu. "Start fresh" is used only when the migration is
// pending and there is nothing unclear to clean up; otherwise the opt-out is
// worded "Dismiss" because it removes the unclear private volumes.
func offerChoices(m state.Migration, unclearCount int) []termio.Choice {
	copyLabel := "Copy the volumes"
	copyDescription := "carry your old sandbox home data into agents-sandbox"
	dismissLabel := "Start fresh"
	dismissDescription := "never offer again; leave the old volumes untouched"
	quitDescription := "abort this session; ask again next time"

	if unclearCount > 0 {
		copyLabel = "Copy the missing volumes; dismiss the existing private volumes from the migration"
		copyDescription = "copy the absent volumes; the unclear private volumes are left untouched"
	}
	if m.Status != "" || unclearCount > 0 {
		dismissLabel = "Dismiss"
		dismissDescription = "never offer again; remove the unclear private volumes"
		quitDescription = "abort this session; remove the private copies you want re-migrated with " +
			"`agents-sandbox volume remove`, then run again"
	}

	return []termio.Choice{
		{Label: copyLabel, Key: choiceCopy, Description: copyDescription},
		{Label: dismissLabel, Key: choiceFresh, Description: dismissDescription},
		{Label: "Quit", Key: choiceQuit, Description: quitDescription},
	}
}
