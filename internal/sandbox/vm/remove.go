package vm

import (
	"context"
	"fmt"
	"strings"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/naming"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// RemoveSandbox removes a project VM by name. It removes only agents-sandbox
// project VMs, refuses a VM that is not stopped unless force is set, confirms
// before mutating, and drops the persisted project state only when force is set.
// A forced removal stops the VM after confirmation, immediately before removing.
func RemoveSandbox(ctx context.Context, name string, force bool, ui termio.UI) error {
	if !strings.HasPrefix(name, naming.VMPrefix) {
		return fmt.Errorf("%q is not an agents-sandbox project VM", name)
	}
	handle, err := msb.Get().GetSandbox(ctx, name)
	if err != nil {
		return fmt.Errorf("remove sandbox %q: %w", name, err)
	}
	stopped := msb.IsSandboxInactive(handle.Status())
	if !stopped && !force {
		return fmt.Errorf("sandbox %q is not stopped; stop it first or pass --force", name)
	}
	if err := termio.ConfirmAction(ui, fmt.Sprintf("Remove sandbox %q?", name)); err != nil {
		return err
	}
	if !stopped {
		if err := handle.Stop(ctx); err != nil {
			return fmt.Errorf("stop sandbox %q: %w", name, err)
		}
	}
	if err := handle.Remove(ctx); err != nil {
		if msb.IsReplaced(err) {
			return fmt.Errorf("remove sandbox %q: %w", name, err)
		}
		if msb.IsNotFound(err) {
			return dropSandboxState(name, force)
		}
		return fmt.Errorf("remove sandbox %q: %w", name, err)
	}
	return dropSandboxState(name, force)
}

// dropSandboxState removes the persisted per-agent state that points at a
// project VM, but only when force is set. A legacy agent-less or unparsable
// name drops nothing.
func dropSandboxState(name string, force bool) error {
	if !force {
		return nil
	}
	artifact := naming.ArtifactFor(name)
	if artifact.Slug == "" || artifact.Agent == "" {
		return nil
	}
	if err := state.RemoveState(state.Key{Slug: artifact.Slug, Agent: artifact.Agent}); err != nil {
		return fmt.Errorf("remove state for sandbox %q: %w", name, err)
	}
	return nil
}
