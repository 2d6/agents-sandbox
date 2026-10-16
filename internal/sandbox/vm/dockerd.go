package vm

import (
	"context"
	"fmt"
	"time"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/termio"
)

const (
	dockerdBinaryCheckCmd = "command -v dockerd >/dev/null 2>&1"
	dockerdReadyCmd       = "docker info"
	// dockerdCleanupCmd removes containerd/pid/socket residue left by a killed
	// dockerd job. Killing the job tears down dockerd but not necessarily its
	// containerd child, so the residue is cleared explicitly before a restart.
	dockerdCleanupCmd = "pkill containerd >/dev/null 2>&1 || true; " +
		"find /run /var/run -iname 'docker*.pid' -delete >/dev/null 2>&1 || true; " +
		"find /run /var/run \\( -iname '*containerd*.sock' -o -iname '*containerd*.pid' \\) -delete >/dev/null 2>&1 || true"
)

const dockerdBinary = "dockerd"

var (
	dockerdReadyTimeout = 30 * time.Second
	dockerdPollInterval = time.Second
	dockerdArgs         = []string{"-H", "unix:///var/run/docker.sock", "--storage-driver=vfs"}
)

// startDockerdIfPresent starts dockerd inside the VM as a runtime-owned
// detached job if the docker image is in use, or does nothing otherwise. It is
// safe to call on every VM bootstrap because it checks for the dockerd binary
// and handles the case where dockerd is already running.
func startDockerdIfPresent(ctx context.Context, sb msb.Sandbox, ui termio.UI) error {
	out, checkErr := sb.Shell(ctx, dockerdBinaryCheckCmd, msbSdk.WithExecUser(RootUser))
	if checkErr != nil {
		return fmt.Errorf("while checking dockerd binary: %w", checkErr)
	}
	if !out.Success() {
		ui.Verbosef("/usr/bin/dockerd not present, skipping Docker startup")
		return nil
	}

	if probeHealthy(ctx, sb, dockerdReadyCmd) {
		ui.Verbose("using already running dockerd")
		return nil
	}

	ui.Verbosef("starting dockerd with vfs storage driver")
	killJobsByCommand(ctx, sb, dockerdBinary, dockerdArgs, ui)
	if _, cleanupErr := sb.Shell(ctx, dockerdCleanupCmd, msbSdk.WithExecUser(RootUser)); cleanupErr != nil {
		ui.Verbosef("dockerd residue cleanup failed (continuing): %v", cleanupErr)
	}

	job, startErr := sb.ExecDetached(ctx, dockerdBinary, dockerdArgs, msbSdk.WithExecUser(RootUser))
	if startErr != nil {
		return fmt.Errorf("start dockerd: %w", startErr)
	}
	defer func() {
		if closeErr := job.Close(); closeErr != nil {
			ui.Verbosef("closing dockerd job handle failed: %v", closeErr)
		}
	}()

	probe := func(probeCtx context.Context) bool {
		return probeHealthy(probeCtx, sb, dockerdReadyCmd)
	}
	if readyErr := awaitJobReady(
		ctx,
		job,
		"dockerd",
		probe,
		dockerdReadyTimeout,
		dockerdPollInterval,
	); readyErr != nil {
		return readyErr
	}
	ui.Verbosef("dockerd is ready")
	return nil
}
