package vm

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

// assumeYesUI returns a non-interactive UI that authorizes any confirmation.
func assumeYesUI() *termio.Mock {
	ui := &termio.Mock{}
	ui.SetAssumeYes(true)
	return ui
}

func TestRemoveSandboxRefusesNonProjectName(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: "user-sandbox", Status_: msbSdk.SandboxStatusStopped}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, false, assumeYesUI())
	if err == nil || !strings.Contains(err.Error(), "not an agents-sandbox project VM") {
		t.Fatalf("RemoveSandbox(non-project) = %v, want a prefix refusal", err)
	}
	if handle.DidRmv {
		t.Error("a non-project name must not be removed")
	}
}

func TestRemoveSandboxRefusesRunningWithoutForce(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix + "proj-aBc1234D", Status_: msbSdk.SandboxStatusRunning}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	ui := assumeYesUI()
	err := RemoveSandbox(context.Background(), handle.Name_, false, ui)
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("RemoveSandbox(running, no force) = %v, want a running refusal", err)
	}
	if handle.DidRmv || handle.DidStop {
		t.Error("a running sandbox without --force must be neither stopped nor removed")
	}
	if len(ui.ConfirmCalls) != 0 {
		t.Errorf("the running guard must run before the prompt, got %v", ui.ConfirmCalls)
	}
}

func TestRemoveSandboxForceStopsThenRemoves(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix + "proj-aBc1234D", Status_: msbSdk.SandboxStatusRunning}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(running, force) = %v", err)
	}
	if !handle.DidStop {
		t.Error("a forced running removal must stop the sandbox first")
	}
	if !handle.DidRmv {
		t.Error("a forced running removal must remove the sandbox")
	}
}

func TestRemoveSandboxForceDropsState(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	key := state.Key{Slug: "proj-1mjusbm3wikhb0", Agent: "opencode"}
	if err := state.WriteState(key, state.HomeState{}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	handle := &msb.MockSandboxHandle{
		Name_:   naming.VMPrefix + key.Slug + "-" + key.Agent,
		Status_: msbSdk.SandboxStatusStopped,
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(force) = %v", err)
	}
	if _, err := state.ReadState(key); !errors.Is(err, state.ErrStateNotFound) {
		t.Fatalf("ReadState after forced removal = %v, want ErrStateNotFound", err)
	}
}

func TestRemoveSandboxWithoutForceKeepsState(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	key := state.Key{Slug: "proj-1mjusbm3wikhb0", Agent: "opencode"}
	if err := state.WriteState(key, state.HomeState{}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	handle := &msb.MockSandboxHandle{
		Name_:   naming.VMPrefix + key.Slug + "-" + key.Agent,
		Status_: msbSdk.SandboxStatusStopped,
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, false, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(no force) = %v", err)
	}
	if _, err := state.ReadState(key); err != nil {
		t.Fatalf("state must survive a non-forced removal, got %v", err)
	}
}

func TestRemoveSandboxReplacedFailsClosed(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	replaced := &msbSdk.Error{Kind: msbSdk.ErrSandboxReplaced, Message: "handle was replaced"}
	handle := &msb.MockSandboxHandle{
		Name_:     naming.VMPrefix + "proj-aBc1234D",
		Status_:   msbSdk.SandboxStatusStopped,
		RemoveErr: replaced,
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI())
	if err == nil || !msb.IsReplaced(err) {
		t.Fatalf("RemoveSandbox(replaced) = %v, want a fail-closed replaced error", err)
	}
	if handle.DidRmv {
		t.Error("a replaced handle must not mark the sandbox removed")
	}
}

func TestRemoveSandboxNotFoundDropsStateWhenForced(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	key := state.Key{Slug: "proj-1mjusbm3wikhb0", Agent: "opencode"}
	if err := state.WriteState(key, state.HomeState{}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	handle := &msb.MockSandboxHandle{
		Name_:     naming.VMPrefix + key.Slug + "-" + key.Agent,
		Status_:   msbSdk.SandboxStatusStopped,
		RemoveErr: &msbSdk.Error{Kind: msbSdk.ErrSandboxNotFound, Message: "no such sandbox"},
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(not found, force) = %v", err)
	}
	if _, err := state.ReadState(key); !errors.Is(err, state.ErrStateNotFound) {
		t.Fatalf("ReadState after a not-found forced removal = %v, want ErrStateNotFound", err)
	}
}

func TestRemoveSandboxSurfacesGetError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	client := &msb.MockMsbClient{}
	client.SetGetSandboxErr(errors.New("boom"))
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), naming.VMPrefix+"proj-aBc1234D", false, assumeYesUI())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("RemoveSandbox(get error) = %v, want the GetSandbox error", err)
	}
}

func TestRemoveSandboxSurfacesStopError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{
		Name_:   naming.VMPrefix + "proj-aBc1234D",
		Status_: msbSdk.SandboxStatusRunning,
		StopErr: errors.New("stop failed"),
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI())
	if err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("RemoveSandbox(stop error) = %v, want the stop error", err)
	}
	if handle.DidRmv {
		t.Error("a failed stop must prevent removal")
	}
}

func TestRemoveSandboxSurfacesRemoveError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{
		Name_:     naming.VMPrefix + "proj-aBc1234D",
		Status_:   msbSdk.SandboxStatusStopped,
		RemoveErr: errors.New("remove failed"),
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, false, assumeYesUI())
	if err == nil || !strings.Contains(err.Error(), "remove failed") {
		t.Fatalf("RemoveSandbox(remove error) = %v, want the remove error", err)
	}
	if msb.IsReplaced(err) || msb.IsNotFound(err) {
		t.Errorf("a generic remove error must not be classified as replaced/not-found: %v", err)
	}
}

func TestRemoveSandboxLegacyAgentlessNameDropsNoState(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix + "proj-aBc1234D", Status_: msbSdk.SandboxStatusStopped}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(legacy name, force) = %v", err)
	}
	if !handle.DidRmv {
		t.Error("a legacy agent-less VM must still be removed")
	}
}

func TestRemoveSandboxBarePrefixDropsNoState(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix, Status_: msbSdk.SandboxStatusStopped}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	if err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI()); err != nil {
		t.Fatalf("RemoveSandbox(bare prefix, force) = %v", err)
	}
}

func TestRemoveSandboxSurfacesStateDropError(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	key := state.Key{Slug: "proj-1mjusbm3wikhb0", Agent: "opencode"}
	slugDir := filepath.Dir(state.KeyDir(key))
	if err := os.MkdirAll(filepath.Dir(slugDir), 0o750); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	if err := os.WriteFile(slugDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block state dir: %v", err)
	}
	handle := &msb.MockSandboxHandle{
		Name_:   naming.VMPrefix + key.Slug + "-" + key.Agent,
		Status_: msbSdk.SandboxStatusStopped,
	}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, true, assumeYesUI())
	if err == nil || !strings.Contains(err.Error(), "remove state") {
		t.Fatalf("RemoveSandbox(state drop error) = %v, want a state-drop error", err)
	}
}

func TestRemoveSandboxNonInteractiveRequiresConfirmation(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix + "proj-aBc1234D", Status_: msbSdk.SandboxStatusStopped}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	err := RemoveSandbox(context.Background(), handle.Name_, false, &termio.Mock{})
	if !errors.Is(err, termio.ErrConfirmationRequired) {
		t.Fatalf("RemoveSandbox(non-interactive) = %v, want ErrConfirmationRequired", err)
	}
	if handle.DidRmv {
		t.Error("the sandbox must not be removed without confirmation")
	}
}

func TestRemoveSandboxDeclinedConfirmation(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	handle := &msb.MockSandboxHandle{Name_: naming.VMPrefix + "proj-aBc1234D", Status_: msbSdk.SandboxStatusStopped}
	client := &msb.MockMsbClient{}
	client.SetGotSandbox(handle)
	msb.WithMsbMock(t, client)

	ui := &termio.Mock{IsInteractiveResult: true, ConfirmResult: false}
	err := RemoveSandbox(context.Background(), handle.Name_, false, ui)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("RemoveSandbox(declined) = %v, want cancelled", err)
	}
	if handle.DidRmv {
		t.Error("a declined confirmation must not remove the sandbox")
	}
}
