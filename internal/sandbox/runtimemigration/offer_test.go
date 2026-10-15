package runtimemigration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// interactiveUI returns an interactive mock UI whose Select returns key and
// records that a prompt happened.
func interactiveUI(key string, prompt *bool) *termio.Mock {
	return &termio.Mock{
		IsInteractiveResult: true,
		SelectFn: func(_ string, _ []termio.Choice, _ string) (string, error) {
			*prompt = true
			return key, nil
		},
	}
}

// writeCorruptRootState writes an unparseable root state.yaml so ReadRootState
// fails, exercising the offer's best-effort error handling.
func writeCorruptRootState(t *testing.T) {
	t.Helper()
	path := filepath.Join(configpaths.Get().UserStateDir(), "state.yaml")
	if err := os.WriteFile(path, []byte("runtime_home_migration: [unterminated"), 0o600); err != nil {
		t.Fatalf("write corrupt root state: %v", err)
	}
}

// TestOfferStateReadErrorWarnsAndSkips pins the spec's anti-goal: a corrupt or
// unreadable launcher-global state file must not block the session. The offer
// is best-effort, so it warns and returns nil rather than surfacing the error.
func TestOfferStateReadErrorWarnsAndSkips(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	writeCorruptRootState(t)

	selectCalls := 0
	ui := &termio.Mock{
		IsInteractiveResult: true,
		SelectFn: func(_ string, _ []termio.Choice, _ string) (string, error) {
			selectCalls++
			return choiceCopy, nil
		},
	}
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v, want nil (best-effort)", err)
	}
	if selectCalls != 0 {
		t.Errorf("Select called %d times, want 0", selectCalls)
	}
	if len(ui.WarnCalls) != 1 {
		t.Errorf("WarnCalls = %v, want exactly one warning", ui.WarnCalls)
	}
	if len(client.Volumes) != 0 || len(client.RemovedVolumes) != 0 {
		t.Error("Offer() touched volumes on a state read error")
	}
}

// TestOfferNecessaryVolumesErrorWarnsAndSkips covers the other best-effort
// branch: a failure deriving necessity (here, the home directory cannot be
// resolved) warns and skips instead of blocking the session.
func TestOfferNecessaryVolumesErrorWarnsAndSkips(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	t.Setenv("HOME", "")

	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v, want nil (best-effort)", err)
	}
	if prompted {
		t.Error("Offer() prompted on a NecessaryVolumes error")
	}
	if len(ui.WarnCalls) != 1 {
		t.Errorf("WarnCalls = %v, want exactly one warning", ui.WarnCalls)
	}
}

func TestOfferDismissedDoesNotPrompt(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")
	if err := state.WriteRootState(state.RootState{
		Migrations: map[string]state.Migration{MigrationName: {Status: state.MigrationDismissed}},
	}); err != nil {
		t.Fatalf("WriteRootState: %v", err)
	}

	var prompted bool
	ui := interactiveUI("c", &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}
	if prompted {
		t.Error("Offer() prompted although the migration was dismissed")
	}
	if len(client.CreatedSandboxes) != 0 {
		t.Error("Offer() copied volumes although the migration was dismissed")
	}
}

func TestOfferNothingNecessaryDoesNotPrompt(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	installLegacyHome(t)

	var prompted bool
	ui := interactiveUI("c", &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}
	if prompted {
		t.Error("Offer() prompted although nothing was necessary")
	}
	if len(ui.WarnCalls) != 0 {
		t.Errorf("Offer() warned despite nothing necessary: %v", ui.WarnCalls)
	}
}

// TestOfferNonInteractiveBlocksBeforeFreshHome pins option D: a non-interactive
// session with a pending migration must not proceed, because resolving the home
// volume would create a fresh one and overwrite the project's recorded
// reference before the user can accept the copy. It returns the pending error
// and touches no volumes.
func TestOfferNonInteractiveBlocksBeforeFreshHome(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	ui := &termio.Mock{IsInteractiveResult: false}
	client := &msb.MockMsbClient{}

	err := Offer(context.Background(), client, ui)
	if !errors.Is(err, ErrMigrationPending) {
		t.Fatalf("Offer() error = %v, want ErrMigrationPending", err)
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("error %q does not mention running interactively", err.Error())
	}
	if len(client.RemovedVolumes) != 0 || len(client.Volumes) != 0 {
		t.Error("Offer() touched volumes in a non-interactive session")
	}
}

// TestOfferAssumeYesBehavesLikeNonInteractive pins Review Focus 3: --yes is
// treated as non-interactive for this offer, so no prompt is ever shown even
// though the user "assumed yes" globally.
func TestOfferAssumeYesBehavesLikeNonInteractive(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	var selectCalls int
	ui := &termio.Mock{
		IsInteractiveResult: false,
		SelectFn: func(_ string, _ []termio.Choice, _ string) (string, error) {
			selectCalls++
			return "c", nil
		},
	}
	ui.SetAssumeYes(true)
	client := &msb.MockMsbClient{}

	err := Offer(context.Background(), client, ui)
	if !errors.Is(err, ErrMigrationPending) {
		t.Fatalf("Offer() error = %v, want ErrMigrationPending under --yes", err)
	}
	if selectCalls != 0 {
		t.Errorf("Select called %d times under --yes, want 0", selectCalls)
	}
}

func TestOfferChooseCopyCopiesVolumes(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "legacy"
	addVolumeDir(t, legacy, name)
	writeFileWithMode(t, legacy+"/volumes/"+name+"/settings.json", "{}", 0o600)

	target := t.TempDir()
	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, volumeName string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			return &msb.MockVolumeHandle{Name_: volumeName, Path_: target}, nil
		},
	}

	var prompted bool
	ui := interactiveUI("c", &prompted)

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}
	if !prompted {
		t.Fatal("Offer() did not prompt in an interactive session with a necessary volume")
	}
	if len(client.RemovedVolumes) != 0 {
		t.Errorf("RemovedVolumes = %v, want none (copy succeeded)", client.RemovedVolumes)
	}
	assertFileWithMode(t, target+"/settings.json", "{}", 0o600)
}

// TestOfferCopyErrorAborts verifies that a failed copy aborts the session rather
// than continuing to a fresh home, which would overwrite the recorded reference
// and orphan the old data. The partial volume is already removed, so necessity
// stays true and the offer returns next session.
func TestOfferCopyErrorAborts(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, _ string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			return nil, context.Canceled
		},
	}

	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)

	if err := Offer(context.Background(), client, ui); err == nil {
		t.Fatal("Offer() error = nil, want a copy failure to abort the session")
	}
}

func TestOfferChooseStartFreshPersistsDismissal(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	var prompted bool
	ui := interactiveUI("s", &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}

	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if m, ok := root.Migration(MigrationName); !ok || m.Status != state.MigrationDismissed {
		t.Error("start-fresh did not persist a dismissed runtime_home migration")
	}
}

// TestOfferChooseQuitAborts pins that quitting the prompt aborts the session
// without a decision: no copy, no dismissal, and the migration stays pending.
func TestOfferChooseQuitAborts(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	var prompted bool
	ui := interactiveUI(choiceQuit, &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); !errors.Is(err, ErrMigrationPending) {
		t.Fatalf("Offer() error = %v, want ErrMigrationPending on quit", err)
	}

	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if m, ok := root.Migration(MigrationName); ok && m.Status == state.MigrationDismissed {
		t.Error("quit dismissed the migration; want it still pending")
	}
}

// TestOfferSelectErrorAborts verifies that a failing prompt (e.g. the terminal
// closed) aborts the session rather than proceeding to a fresh home.
func TestOfferSelectErrorAborts(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	ui := &termio.Mock{
		IsInteractiveResult: true,
		SelectFn: func(_ string, _ []termio.Choice, _ string) (string, error) {
			return "", context.Canceled
		},
	}
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); !errors.Is(err, ErrMigrationPending) {
		t.Fatalf("Offer() error = %v, want ErrMigrationPending on prompt failure", err)
	}
	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if m, ok := root.Migration(MigrationName); ok && m.Status == state.MigrationDismissed {
		t.Error("a failed prompt dismissed the migration; want it still pending")
	}
	if len(client.RemovedVolumes) != 0 || len(client.Volumes) != 0 {
		t.Error("a failed prompt touched volumes")
	}
}

// TestOfferChoicesAreCopyFreshQuit pins that "decide later" is gone: the only
// outcomes are copy, start fresh, or quit.
func TestOfferChoicesAreCopyFreshQuit(t *testing.T) {
	choices := offerChoices()
	got := make([]string, len(choices))
	for i, c := range choices {
		got[i] = c.Key
	}
	assertSameSet(t, got, []string{choiceCopy, choiceFresh, choiceQuit})
}
