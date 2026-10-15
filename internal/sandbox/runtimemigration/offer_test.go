package runtimemigration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/runtime"
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
	if err := os.WriteFile(path, []byte("migrations: [unterminated"), 0o600); err != nil {
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

// TestOfferTerminalMigrationDoesNotPrompt pins that a terminal (completed)
// migration is never re-armed: even with a legacy volume present, the offer
// returns without prompting.
func TestOfferTerminalMigrationDoesNotPrompt(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")
	if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
		MigrationName: {Status: state.MigrationCompleted},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)
	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if prompted {
		t.Error("prompted after a terminal migration")
	}
}

// TestOfferFinalizesOpenCampaignWithNothingLeft pins the crash-recovery finalize:
// a campaign that copied everything but crashed before recording completion is
// closed out as completed on the next offer, without prompting.
func TestOfferFinalizesOpenCampaignWithNothingLeft(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	name := naming.HomePrefix + "done"
	addVolumeDir(t, legacy, name)
	// Campaign copied everything, then crashed before writing completed.
	if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
		MigrationName: {Status: state.MigrationCopying, Copied: []string{name}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ui := &termio.Mock{IsInteractiveResult: true}
	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	root, _ := state.ReadRootState()
	m, _ := root.Migration(MigrationName)
	if m.Status != state.MigrationCompleted {
		t.Fatalf("status = %q, want completed", m.Status)
	}
}

// TestOfferStartFreshIsNotLostToACompletedCampaign pins the lock-serialization
// ruling: a "start fresh" dismissal must not clobber a terminal status another
// session already recorded. The migration is pending when the prompt is shown,
// but a concurrent session completes it before the dismissal write; choosing
// fresh must re-read under the lock and leave the terminal completed status
// intact.
func TestOfferStartFreshIsNotLostToACompletedCampaign(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	// A concurrent session wins the race by completing the campaign after the
	// prompt is shown but before the dismissal write.
	var prompted bool
	ui := interactiveUI(choiceFresh, &prompted)
	ui.SelectFn = func(_ string, _ []termio.Choice, _ string) (string, error) {
		prompted = true
		root, err := state.ReadRootState()
		if err != nil {
			t.Fatalf("ReadRootState: %v", err)
		}
		if m, _ := root.Migration(MigrationName); m.Status != "" {
			t.Fatalf("precondition: status = %q, want pending", m.Status)
		}
		if err := state.WriteRootState(state.RootState{Migrations: map[string]state.Migration{
			MigrationName: {Status: state.MigrationCompleted},
		}}); err != nil {
			t.Fatalf("concurrent completion: %v", err)
		}
		return choiceFresh, nil
	}

	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if !prompted {
		t.Fatal("Offer did not prompt")
	}
	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if m, _ := root.Migration(MigrationName); m.Status != state.MigrationCompleted {
		t.Fatalf("status = %q, want completed (start-fresh must not overwrite a finished campaign)", m.Status)
	}
}

// TestOfferStartFreshLockFailureSurfaces pins that a migration-lock acquisition
// failure on the start-fresh path surfaces as an Offer error rather than a
// silent no-op. A regular file occupying $MSB_HOME/.agents-sandbox makes the
// lock directory creation fail.
func TestOfferStartFreshLockFailureSurfaces(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	privateHome := runtime.PrivateHome()
	if err := os.MkdirAll(privateHome, 0o750); err != nil {
		t.Fatalf("mkdir private home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(privateHome, ".agents-sandbox"), []byte("x"), 0o600); err != nil {
		t.Fatalf("block lock dir: %v", err)
	}

	var prompted bool
	ui := interactiveUI(choiceFresh, &prompted)
	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err == nil {
		t.Fatal("Offer() error = nil, want a lock-acquisition failure on start-fresh")
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

// TestOfferCopyErrorAborts verifies that a campaign-scoped copy failure aborts
// the session rather than continuing to a fresh home, which would overwrite the
// recorded reference and orphan the old data. Here the state root is unwritable,
// so persisting the copying status fails and the error propagates through Offer.
// A volume-scoped copy failure, by contrast, is excluded inside CopyVolumes and
// does not abort (see TestCopyVolumesExcludesVolumeScopedFailureAndCompletes).
func TestOfferCopyErrorAborts(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")
	blockRootState(t)

	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)

	if err := Offer(context.Background(), &msb.MockMsbClient{}, ui); err == nil {
		t.Fatal("Offer() error = nil, want a campaign-scoped copy failure to abort the session")
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

// makeLegacyVolumesUnlistable replaces <legacy>/volumes with a self-referential
// symlink, so listing it fails with ELOOP (a non-tolerated error in both the
// pending and copying listings).
func makeLegacyVolumesUnlistable(t *testing.T, legacy string) {
	t.Helper()
	volumes := filepath.Join(legacy, "volumes")
	if err := os.RemoveAll(volumes); err != nil {
		t.Fatalf("remove volumes dir: %v", err)
	}
	if err := os.Symlink("volumes", volumes); err != nil {
		t.Fatalf("symlink volumes: %v", err)
	}
}

// TestOfferChoicesAreCopyFreshQuit pins that "decide later" is gone: the only
// outcomes are copy, start fresh, or quit.
func TestOfferChoicesAreCopyFreshQuit(t *testing.T) {
	choices := offerChoices(state.Migration{}, 0)
	got := make([]string, len(choices))
	for i, c := range choices {
		got[i] = c.Key
	}
	assertSameSet(t, got, []string{choiceCopy, choiceFresh, choiceQuit})
}

func choiceLabels(choices []termio.Choice) []string {
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = c.Label
	}
	return labels
}

// TestOfferChoicesWording pins the label rule: "Start fresh" only when the
// migration is pending and there is nothing unclear to clean up; otherwise the
// opt-out is worded "Dismiss".
func TestOfferChoicesWording(t *testing.T) {
	t.Run("pending no unclear is start fresh", func(t *testing.T) {
		labels := choiceLabels(offerChoices(state.Migration{}, 0))
		if !slices.Contains(labels, "Start fresh") || slices.Contains(labels, "Dismiss") {
			t.Errorf("labels = %v, want Start fresh, not Dismiss", labels)
		}
	})
	t.Run("pending with unclear is dismiss", func(t *testing.T) {
		labels := choiceLabels(offerChoices(state.Migration{}, 2))
		if !slices.Contains(labels, "Dismiss") || slices.Contains(labels, "Start fresh") {
			t.Errorf("labels = %v, want Dismiss, not Start fresh", labels)
		}
	})
	t.Run("copying is dismiss", func(t *testing.T) {
		labels := choiceLabels(offerChoices(state.Migration{Status: state.MigrationCopying}, 0))
		if !slices.Contains(labels, "Dismiss") || slices.Contains(labels, "Start fresh") {
			t.Errorf("labels = %v, want Dismiss, not Start fresh", labels)
		}
	})
}

// TestOfferPromptIncludesUnclearNotice pins the ambiguous-state instructions: the
// prompt explains the unclear volumes and the `volume remove --force` cleanup
// path, and omits them when there is nothing unclear.
func TestOfferPromptIncludesUnclearNotice(t *testing.T) {
	plain := offerPrompt(1, 0)
	if strings.Contains(plain, "unclear") {
		t.Errorf("plain prompt mentions unclear volumes: %q", plain)
	}
	noted := offerPrompt(1, 2)
	for _, want := range []string{"unclear", "volume remove --force", "Quit"} {
		if !strings.Contains(noted, want) {
			t.Errorf("prompt %q missing %q", noted, want)
		}
	}
}

// TestOfferAmbiguousStateCopyKeepsExisting pins option 1 in the ambiguous state:
// a present private volume of unclear status is recorded as Kept and left
// untouched while only the missing volumes are copied.
func TestOfferAmbiguousStateCopyKeepsExisting(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	present := naming.HomePrefix + "present"
	missing := naming.HomePrefix + "missing"
	addVolumeDir(t, legacy, present)
	addVolumeDir(t, legacy, missing)
	if err := os.MkdirAll(filepath.Join(runtime.PrivateHome(), "volumes", present), 0o750); err != nil {
		t.Fatalf("seed present private volume: %v", err)
	}

	target := t.TempDir()
	var created []string
	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, n string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			created = append(created, n)
			return &msb.MockVolumeHandle{Name_: n, Path_: target}, nil
		},
	}
	var prompted bool
	ui := interactiveUI(choiceCopy, &prompted)
	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if !prompted {
		t.Fatal("Offer did not prompt in the ambiguous state")
	}
	if len(created) != 1 || created[0] != missing {
		t.Errorf("created = %v, want only %q", created, missing)
	}
	if slices.Contains(client.RemovedVolumes, present) {
		t.Errorf("RemovedVolumes = %v, want present %q kept", client.RemovedVolumes, present)
	}
	root, _ := state.ReadRootState()
	if got, _ := root.Migration(MigrationName); got.Status != state.MigrationCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
}
