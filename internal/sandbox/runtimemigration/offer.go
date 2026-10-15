package runtimemigration

import (
	"context"
	"errors"
	"fmt"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// Offer choices. "copy" copies every necessary legacy volume at once; "fresh"
// records a permanent opt-out; "quit" aborts the session without a decision, so
// the offer returns next time. There is deliberately no "decide later": the
// session cannot proceed while the decision is unmade, because resolving the
// home volume would create a fresh one and overwrite the project's recorded
// reference before the old data could be copied.
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
// The offer is global: one acceptance copies every necessary volume. It is
// shown only while the migration is both necessary and undismissed.
//
// A non-interactive session is never prompted and never copies: because
// proceeding would overwrite the project's recorded home-volume reference
// before the copy can happen, it returns ErrMigrationPending with guidance
// instead. A failure to read the persisted state or derive necessity is
// best-effort: it warns and skips rather than blocking the session. A failed
// copy aborts the session (ErrMigrationPending): continuing would orphan the
// old data, while the per-volume copy already removed any partial volume so
// necessity stays true and the offer returns next session.
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
		// Likewise, a failure deriving necessity warns and skips rather than
		// blocking the session.
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	if len(names) == 0 {
		if migration.Status == state.MigrationCopying {
			return finalizeCompletedCampaign()
		}
		return nil
	}

	if !ui.IsInteractive() {
		return fmt.Errorf(
			"%w: %d legacy microsandbox home volume(s) can be copied into agents-sandbox; "+
				"run an interactive `run`/`shell` session to copy them or start fresh",
			ErrMigrationPending, len(names),
		)
	}

	choice, err := ui.Select(offerPrompt(len(names)), offerChoices(), choiceQuit)
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
		return dismissMigration()
	default:
		return ErrMigrationPending
	}
}

// migrationStatus returns the recorded status of the runtime-home migration, or
// the empty (pending) status when no entry exists.
func migrationStatus(root *state.RootState) state.MigrationStatus {
	m, _ := root.Migration(MigrationName)
	return m.Status
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
		if migrationStatus(root) != state.MigrationCopying {
			return nil
		}
		return saveMigration(state.Migration{Status: state.MigrationCompleted})
	})
}

// dismissMigration records the permanent start-fresh opt-out under the lock,
// re-reading first so a concurrent campaign's terminal write (completed or
// dismissed) is not overwritten.
func dismissMigration() error {
	return withMigrationLock(func() error {
		root, err := state.ReadRootState()
		if err != nil {
			return err
		}
		status := migrationStatus(root)
		if status == state.MigrationCompleted || status == state.MigrationDismissed {
			return nil
		}
		return saveMigration(state.Migration{Status: state.MigrationDismissed})
	})
}

func offerPrompt(count int) string {
	return fmt.Sprintf("Copy %d legacy home volume(s) into agents-sandbox?", count)
}

func offerChoices() []termio.Choice {
	return []termio.Choice{
		{
			Label:       "Copy the volumes",
			Key:         choiceCopy,
			Description: "carry your old sandbox home data into agents-sandbox",
		},
		{Label: "Start fresh", Key: choiceFresh, Description: "never offer again; leave the old volumes untouched"},
		{Label: "Quit", Key: choiceQuit, Description: "abort this session; ask again next time"},
	}
}
