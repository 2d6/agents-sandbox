package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/inoio/agents-sandbox/internal/configpaths"
)

func TestPrivateHomeUsesDataDir(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	got, err := PrivateHome()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configpaths.Get().UserDataDir(), "msb")
	if got != want || filepath.Base(got) != "msb" {
		t.Errorf("PrivateHome() = %q, want %q", got, want)
	}
}

func TestApplyHomeSetsMSBHome(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	unsetEnv(t, "MSB_HOME")
	home, err := ApplyHome()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MSB_HOME") != home {
		t.Errorf("MSB_HOME = %q, want %q", os.Getenv("MSB_HOME"), home)
	}
}

func TestLegacyHomeUsesUserHome(t *testing.T) {
	oldHome := userHomeDir
	userHomeDir = func() (string, error) { return "/home/alice", nil }
	t.Cleanup(func() { userHomeDir = oldHome })

	got, err := LegacyHome()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/alice", ".microsandbox"); got != want {
		t.Errorf("LegacyHome() = %q, want %q", got, want)
	}
}

func TestLegacyHomePropagatesHomeError(t *testing.T) {
	oldHome := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	t.Cleanup(func() { userHomeDir = oldHome })

	if _, err := LegacyHome(); err == nil {
		t.Fatal("LegacyHome() error = nil, want home resolution error")
	}
}
