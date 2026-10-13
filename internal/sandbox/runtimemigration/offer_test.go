package runtimemigration

import (
	"context"
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
		RuntimeHomeMigration: state.RuntimeHomeMigration{Dismissed: true},
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

func TestOfferNonInteractiveWarnsAndNeverCopies(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	ui := &termio.Mock{IsInteractiveResult: false}
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}
	if len(ui.WarnCalls) != 1 {
		t.Fatalf("WarnCalls = %v, want exactly one reminder", ui.WarnCalls)
	}
	if !strings.Contains(ui.WarnCalls[0], "interactive") {
		t.Errorf("warning %q does not mention running interactively", ui.WarnCalls[0])
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

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}
	if selectCalls != 0 {
		t.Errorf("Select called %d times under --yes, want 0", selectCalls)
	}
	if len(ui.WarnCalls) != 1 {
		t.Errorf("WarnCalls = %v, want exactly one reminder under --yes", ui.WarnCalls)
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
	if !root.RuntimeHomeMigration.Dismissed {
		t.Error("start-fresh did not persist RuntimeHomeMigration.Dismissed")
	}
}

func TestOfferChooseLaterLeavesStateUnchanged(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	legacy := installLegacyHome(t)
	addVolumeDir(t, legacy, naming.HomePrefix+"legacy")

	var prompted bool
	ui := interactiveUI("q", &prompted)
	client := &msb.MockMsbClient{}

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v", err)
	}

	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if root.RuntimeHomeMigration.Dismissed {
		t.Error("choose-later dismissed the migration; want it offered again next time")
	}
}

// TestOfferSelectErrorLeavesStateUnchanged verifies that a failing prompt (e.g.
// the terminal closed) is treated as "ask later": no copy, no dismissal.
func TestOfferSelectErrorLeavesStateUnchanged(t *testing.T) {
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

	if err := Offer(context.Background(), client, ui); err != nil {
		t.Fatalf("Offer() error = %v, want nil on prompt failure", err)
	}
	root, err := state.ReadRootState()
	if err != nil {
		t.Fatalf("ReadRootState: %v", err)
	}
	if root.RuntimeHomeMigration.Dismissed {
		t.Error("a failed prompt dismissed the migration; want it offered again")
	}
	if len(client.RemovedVolumes) != 0 || len(client.Volumes) != 0 {
		t.Error("a failed prompt touched volumes")
	}
}
