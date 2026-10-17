package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/git"
	"github.com/inoio/agents-sandbox/internal/sandbox/docker"
	"github.com/inoio/agents-sandbox/internal/sandbox/doctor"
	"github.com/inoio/agents-sandbox/internal/sandbox/image"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// TestVolumeOpsKeepDeprecatedRebuildFlag verifies the three volume ops keep
// --rebuild as a hidden deprecated flag, so the 0.5.0 spelling is not a silent
// break while it waits out its removal window.
func TestVolumeOpsKeepDeprecatedRebuildFlag(t *testing.T) {
	cmd, _ := setupCommandFixtures(t)
	for _, name := range []string{cmdMigrate, cmdReset, cmdEdit} {
		sub, _, err := cmd.Find([]string{cmdVolume, name})
		if err != nil {
			t.Fatalf("find volume %s: %v", name, err)
		}
		flag := sub.Flags().Lookup(flagRebuild)
		if flag == nil || flag.Deprecated == "" {
			t.Errorf("volume %s must keep --rebuild as a deprecated no-op", name)
			continue
		}
		if !flag.Hidden {
			t.Errorf("volume %s --rebuild must be hidden", name)
		}
	}
}

// TestVolumeMigrateRebuildIsDeprecatedNoOp verifies passing the deprecated
// --rebuild still succeeds (it is a no-op) and warns once, so scripts using the
// 0.5.0 spelling keep working.
func TestVolumeMigrateRebuildIsDeprecatedNoOp(t *testing.T) {
	initTestProjectDir(t)
	configpaths.WithMockConfigPaths(t)
	ui := &termio.Mock{}
	mock := &msb.MockMsbClient{}
	doctor.MockedCheckAll(t, true)
	image.WithMockAgentVersion(t, "0.0.0-test")
	docker.WithNoopDockerMock(t)
	slug := git.ProjectSlug()
	state.WriteState(state.Key{Slug: slug, Agent: "opencode"}, state.HomeState{
		HomeVolume: fmt.Sprintf("agents-sandbox-home-%s-20260101T000000", slug),
	})
	msb.WithMsbMock(t, mock)

	root := buildRootCmd(ui)
	root.SetOut(ui.StdOut())
	root.SetErr(ui.StdErr())
	root.SetArgs([]string{"volume", "migrate", "explicit-vol-name", "--rebuild"})
	if err := root.Execute(); err != nil {
		t.Fatalf("volume migrate --rebuild: %v", err)
	}
	captured := ui.StdOutBuffer.String() + ui.StdErrBuffer.String()
	if !strings.Contains(captured, "--rebuild has been deprecated") {
		t.Errorf("expected a --rebuild deprecation warning, got output %q", captured)
	}
}
