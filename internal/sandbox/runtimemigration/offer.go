package runtimemigration

import (
	"context"
	"fmt"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// Offer choices. "copy" copies every necessary legacy volume at once; "fresh"
// records a permanent opt-out; "later" leaves the offer pending for the next
// interactive session.
const (
	choiceCopy  = "c"
	choiceFresh = "s"
	choiceLater = "q"
)

// Offer presents the one-time, opt-in copy of legacy microsandbox home volumes
// into the private runtime home, at the start of an interactive session.
//
// The offer is global: one acceptance copies every necessary volume. It is
// shown only while the migration is both necessary and undismissed. A
// non-interactive session (including --yes, which IsInteractive reports false
// for) is never prompted and never copies; it gets a one-line reminder instead.
// A failure to read the persisted state or derive necessity is best-effort: it
// warns and skips rather than aborting the session. A failed copy is reported
// but does not abort the session either: the per-volume copy already removes
// any partial volume, so necessity stays true and the offer returns on the next
// interactive start.
func Offer(ctx context.Context, client msb.Client, ui termio.UI) error {
	rootState, err := state.ReadRootState()
	if err != nil {
		// The copy is a best-effort convenience: a corrupt or unreadable
		// launcher-global state file must not block the agent from running.
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	if rootState.RuntimeHomeMigration.Dismissed {
		return nil
	}

	names, err := NecessaryVolumes()
	if err != nil {
		// Likewise, a failure deriving necessity warns and skips rather than
		// aborting the session.
		ui.Warnf("skipping legacy home-volume copy offer: %v", err)
		return nil
	}
	if len(names) == 0 {
		return nil
	}

	if !ui.IsInteractive() {
		ui.Warnf(
			"%d legacy microsandbox home volume(s) can be copied into agents-sandbox; run in an interactive session to copy them",
			len(names),
		)
		return nil
	}

	choice, err := ui.Select(offerPrompt(len(names)), offerChoices(), choiceLater)
	if err != nil {
		// A failed prompt (e.g. the terminal closed) means "decide later": leave
		// the migration pending so the next interactive session re-offers it.
		choice = choiceLater
	}

	switch choice {
	case choiceCopy:
		if err := CopyVolumes(ctx, client, names, ui); err != nil {
			ui.Errorf("copying legacy home volumes: %v (continuing without them)", err)
		}
		return nil
	case choiceFresh:
		return state.WriteRootState(state.RootState{
			RuntimeHomeMigration: state.RuntimeHomeMigration{Dismissed: true},
		})
	default:
		return nil
	}
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
		{Label: "Decide later", Key: choiceLater, Description: "ask again at the next interactive session"},
	}
}
