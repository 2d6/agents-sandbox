package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/inoio/agents-sandbox/internal/agent"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/termio"
)

var daemonReadyTimeout = 60 * time.Second

var daemonPollInterval = 2 * time.Second

// daemonHealthFunc is the test seam for the daemon health probe, matching the
// ensureInstalled pattern in doctor.go. Tests override this; production code
// leaves the default.
var daemonHealthFunc = func(ctx context.Context, sb msb.Sandbox, command string) (string, int, error) {
	out, err := sb.Shell(ctx, command)
	if err != nil {
		return "", -1, err
	}
	return out.Stdout(), out.ExitCode(), nil
}

// SetDaemonHealthFunc replaces the daemonHealthFunc factory used by
// ensureDaemon with one that returns the provided function. The original
// factory is returned so callers can restore it after their test.
func SetDaemonHealthFunc(
	f func(ctx context.Context, sb msb.Sandbox, command string) (string, int, error),
) func(ctx context.Context, sb msb.Sandbox, command string) (string, int, error) {
	orig := daemonHealthFunc
	daemonHealthFunc = f
	return orig
}

// ensureDaemon guarantees the agent's serve daemon is healthy inside the VM as
// a runtime-owned detached job. It health checks via the provider's command; if
// unhealthy (or force is set, as for a config restart), it kills any stale
// daemon job, launches a fresh one via ExecDetached, and waits until healthy or
// timeout. An agent without a daemon provider is a no-op.
func ensureDaemon(
	ctx context.Context,
	a agent.Agent,
	serveOnly, force bool,
	sb msb.Sandbox,
	ui termio.UI,
) error {
	provider, ok := agent.AsDaemonProvider(a)
	if !ok {
		return nil
	}

	if !force && checkDaemonHealth(ctx, sb, provider) {
		ui.Verbosef("%s daemon already healthy", a.Name())
		return nil
	}

	command := provider.DaemonCommand(serveOnly)
	ui.Verbosef("starting %s serve daemon", a.Name())
	killJobsByCommand(ctx, sb, command.Binary, command.Args, ui)

	job, startErr := sb.ExecDetached(ctx, command.Binary, command.Args)
	if startErr != nil {
		return fmt.Errorf("start %s serve: %w", a.Name(), startErr)
	}
	defer func() {
		if closeErr := job.Close(); closeErr != nil {
			ui.Verbosef("closing %s job handle failed: %v", a.Name(), closeErr)
		}
	}()

	probe := func(probeCtx context.Context) bool {
		return checkDaemonHealth(probeCtx, sb, provider)
	}
	label := a.Name() + " daemon"
	if readyErr := awaitJobReady(ctx, job, label, probe, daemonReadyTimeout, daemonPollInterval); readyErr != nil {
		return readyErr
	}
	ui.Verbosef("%s daemon is healthy", a.Name())
	return nil
}

func checkDaemonHealth(ctx context.Context, sb msb.Sandbox, provider agent.DaemonProvider) bool {
	stdout, exitCode, err := daemonHealthFunc(ctx, sb, provider.DaemonHealthCmd())
	if err != nil || exitCode != 0 {
		return false
	}
	healthy, err := provider.DaemonHealthParse(stdout)
	return err == nil && healthy
}
