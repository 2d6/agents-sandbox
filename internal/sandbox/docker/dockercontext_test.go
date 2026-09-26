package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/moby/client"
)

const colimaSocket = "unix:///Users/tester/.colima/default/docker.sock"

// writeDockerConfig creates a docker config directory with currentContext set
// to name and points DOCKER_CONFIG at it.
func writeDockerConfig(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if name != "" {
		data, err := json.Marshal(map[string]string{"currentContext": name})
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
			t.Fatalf("write config.json: %v", err)
		}
	}
	t.Setenv(envDockerConfig, dir)
	return dir
}

// writeContextMeta stores a context metadata file the way the docker CLI does,
// in a directory named after the sha256 digest of the context name.
func writeContextMeta(t *testing.T, configDir, name, host string) {
	t.Helper()
	digest := sha256.Sum256([]byte(name))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(digest[:]))
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	meta := map[string]any{
		"Name":      name,
		"Endpoints": map[string]any{"docker": map[string]any{"Host": host}},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), data, 0o600); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}

func TestResolveHostFromActiveContext(t *testing.T) {
	t.Setenv(envDockerHost, "")
	configDir := writeDockerConfig(t, "colima")
	writeContextMeta(t, configDir, "colima", colimaSocket)

	if got := resolveHost(); got != colimaSocket {
		t.Errorf("resolveHost() = %q, want %q", got, colimaSocket)
	}
}

func TestResolveHostPrefersDockerHostEnv(t *testing.T) {
	t.Setenv(envDockerHost, "unix:///var/run/docker.sock")
	configDir := writeDockerConfig(t, "colima")
	writeContextMeta(t, configDir, "colima", colimaSocket)

	if got := resolveHost(); got != "" {
		t.Errorf("resolveHost() = %q, want %q so DOCKER_HOST keeps precedence", got, "")
	}
}

func TestResolveHostPrefersDockerContextEnvOverConfig(t *testing.T) {
	const remote = "tcp://docker.example:2376"
	t.Setenv(envDockerHost, "")
	configDir := writeDockerConfig(t, "colima")
	writeContextMeta(t, configDir, "colima", colimaSocket)
	writeContextMeta(t, configDir, "remote", remote)
	t.Setenv(envDockerContext, "remote")

	if got := resolveHost(); got != remote {
		t.Errorf("resolveHost() = %q, want %q", got, remote)
	}
}

func TestResolveHostIgnoresUnusableContexts(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T)
	}{
		{
			name:    "no config file",
			prepare: func(t *testing.T) { writeDockerConfig(t, "") },
		},
		{
			name:    "default context",
			prepare: func(t *testing.T) { writeDockerConfig(t, "default") },
		},
		{
			name: "missing metadata",
			prepare: func(t *testing.T) {
				writeDockerConfig(t, "colima")
			},
		},
		{
			name: "unparsable metadata",
			prepare: func(t *testing.T) {
				configDir := writeDockerConfig(t, "colima")
				digest := sha256.Sum256([]byte("colima"))
				metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(digest[:]))
				if err := os.MkdirAll(metaDir, 0o700); err != nil {
					t.Fatalf("mkdir meta: %v", err)
				}
				if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte("{"), 0o600); err != nil {
					t.Fatalf("write meta.json: %v", err)
				}
			},
		},
		{
			name: "context without docker endpoint",
			prepare: func(t *testing.T) {
				configDir := writeDockerConfig(t, "kube-only")
				writeContextMeta(t, configDir, "kube-only", "")
			},
		},
		{
			name: "unparsable config file",
			prepare: func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{"), 0o600); err != nil {
					t.Fatalf("write config.json: %v", err)
				}
				t.Setenv(envDockerConfig, dir)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envDockerHost, "")
			tt.prepare(t)

			if got := resolveHost(); got != "" {
				t.Errorf("resolveHost() = %q, want %q", got, "")
			}
		})
	}
}

func TestClientOptionsConnectToActiveContext(t *testing.T) {
	t.Setenv(envDockerHost, "")
	configDir := writeDockerConfig(t, "colima")
	writeContextMeta(t, configDir, "colima", colimaSocket)

	c, err := client.New(clientOptions()...)
	if err != nil {
		t.Fatalf("client.New() error = %v, want nil", err)
	}
	t.Cleanup(func() { c.Close() })

	if got := c.DaemonHost(); got != colimaSocket {
		t.Errorf("DaemonHost() = %q, want %q", got, colimaSocket)
	}
}

func TestClientOptionsFallBackToEnvironment(t *testing.T) {
	const envHost = "unix:///Users/tester/.docker/run/docker.sock"
	t.Setenv(envDockerHost, envHost)
	configDir := writeDockerConfig(t, "colima")
	writeContextMeta(t, configDir, "colima", colimaSocket)

	c, err := client.New(clientOptions()...)
	if err != nil {
		t.Fatalf("client.New() error = %v, want nil", err)
	}
	t.Cleanup(func() { c.Close() })

	if got := c.DaemonHost(); got != envHost {
		t.Errorf("DaemonHost() = %q, want %q", got, envHost)
	}
}
