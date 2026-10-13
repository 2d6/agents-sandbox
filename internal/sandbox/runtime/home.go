package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inoio/agents-sandbox/internal/configpaths"
)

// PrivateHome is the microsandbox home agents-sandbox owns: a private directory
// under the tool's data dir, never the user's ~/.microsandbox.
func PrivateHome() (string, error) {
	return filepath.Join(configpaths.Get().UserDataDir(), "msb"), nil
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

// ApplyHome points the microsandbox SDK at the private home for this process.
func ApplyHome() (string, error) {
	home, err := PrivateHome()
	if err != nil {
		return "", err
	}
	if err := os.Setenv("MSB_HOME", home); err != nil {
		return "", err
	}
	return home, nil
}
