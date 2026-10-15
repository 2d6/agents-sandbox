package msb

import (
	"errors"
	"testing"
	"time"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"
)

func TestMockImageHandleNewAccessors(t *testing.T) {
	bytes := int64(123456)
	h := MockImageHandle{
		Reference_:      "agents-sandbox/runner:latest",
		ManifestDigest_: "sha256:abc",
		SizeBytes_:      &bytes,
		CreatedAt_:      time.Date(2026, 8, 17, 10, 42, 36, 0, time.UTC),
	}
	if got := h.SizeBytes(); got == nil || *got != 123456 {
		t.Fatalf("SizeBytes() = %v, want 123456", got)
	}
	if want := "2026-08-17 10:42:36"; h.CreatedAt().Format("2006-01-02 15:04:05") != want {
		t.Fatalf("CreatedAt() = %v, want %s", h.CreatedAt(), want)
	}
	if h.Reference() != "agents-sandbox/runner:latest" || h.ManifestDigest() != "sha256:abc" {
		t.Fatalf("existing accessors broken: %s %s", h.Reference(), h.ManifestDigest())
	}
}

func TestMockSandboxHandleConfig(t *testing.T) {
	cfg := &msbSdk.SandboxConfig{Image: "agents-sandbox/runner-proj:opencode-latest"}
	h := &MockSandboxHandle{Cfg: cfg}
	got, err := h.Config()
	if err != nil || got != cfg {
		t.Fatalf("Config() = (%v, %v), want the configured config", got, err)
	}

	broken := &MockSandboxHandle{ConfigErr: errors.New("sandbox gone")}
	if _, err := broken.Config(); !errors.Is(err, broken.ConfigErr) {
		t.Fatalf("Config() error = %v, want the configured error", err)
	}
}
