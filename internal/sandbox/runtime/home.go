package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inoio/agents-sandbox/internal/configpaths"
)

// PrivateHome is the microsandbox home agents-sandbox owns: a private directory
// under the tool's data dir, never the user's ~/.microsandbox. It derives purely
// from the config paths, so it cannot fail.
func PrivateHome() string {
	return filepath.Join(configpaths.Get().UserDataDir(), "msb")
}

// LegacyHome is the shared microsandbox home a user's own install may occupy.
// It is only read by the opt-in legacy volume copy.
func LegacyHome() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".microsandbox"), nil
}

// setenv is a seam over os.Setenv so tests can exercise the failure path. A
// real Setenv of a valid constant key cannot fail.
var setenv = os.Setenv

// ApplyHome points the microsandbox SDK at the private home for this process.
func ApplyHome() (string, error) {
	home := PrivateHome()
	if err := setenv("MSB_HOME", home); err != nil {
		return "", err
	}
	return home, nil
}
