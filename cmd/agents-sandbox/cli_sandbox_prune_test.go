package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
)

func TestSandboxPrune(t *testing.T) {
	t.Run("prunes stale sandbox", func(t *testing.T) {
		m := &msb.MockMsbClient{
			Sandboxes: []msb.SandboxHandle{
				&msb.MockSandboxHandle{
					Name_:      "agents-sandbox-vm-proj-1mjusbm3wikhb0",
					Status_:    msbSdk.SandboxStatusStopped,
					UpdatedAt_: time.Now().Add(-30 * 24 * time.Hour),
				},
			},
		}
		cmd, ui := setupCommandFixtures(t, "sandbox", "prune")
		msb.WithMsbMock(t, m)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !slices.Contains(ui.OutCalls, "Pruned 1 sandbox(es)") {
			t.Errorf("expected prune summary, got: %v", ui.OutCalls)
		}
	})
}

func TestSandboxPruneFormatJSON(t *testing.T) {
	m := &msb.MockMsbClient{
		Sandboxes: []msb.SandboxHandle{
			&msb.MockSandboxHandle{
				Name_:      "agents-sandbox-vm-proj-1mjusbm3wikhb0",
				Status_:    msbSdk.SandboxStatusStopped,
				UpdatedAt_: time.Now().Add(-30 * 24 * time.Hour),
			},
		},
	}
	cmd, ui := setupCommandFixtures(t, "sandbox", "prune", "--format", "json")
	msb.WithMsbMock(t, m)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got struct {
		DryRun    bool `json:"dry_run"`
		Sandboxes int  `json:"sandboxes"`
		Details   []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"details"`
	}
	joined := strings.Join(ui.OutCalls, "\n")
	if err := json.Unmarshal([]byte(joined), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v; output: %q", err, joined)
	}
	if got.Sandboxes != 1 || len(got.Details) != 1 {
		t.Fatalf("unexpected JSON: %+v", got)
	}
	if got.Details[0].Type != "vm" || got.Details[0].Name == "" {
		t.Errorf("unexpected detail: %+v", got.Details[0])
	}
}
