package image

import (
	"context"
	"io"
	"strings"
	"testing"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/client"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/sandbox/docker"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// TestLegacyDockerfileWarnings verifies that the two legacy dind spellings a
// project Dockerfile can carry each yield a deprecation warning, that the
// docker spelling is silent, and that a plain Dockerfile warns about nothing.
func TestLegacyDockerfileWarnings(t *testing.T) {
	baseWarning := "project Dockerfile uses the deprecated agents-sandbox/runner-base-dind base image; switch to agents-sandbox/runner-base-docker"
	markerWarning := "project Dockerfile uses the deprecated # agents-sandbox:dind marker; switch to # agents-sandbox:docker"

	cases := []struct {
		name    string
		project string
		want    []string
	}{
		{name: "empty", project: "", want: nil},
		{
			name:    "plain base and body",
			project: "FROM debian:trixie-slim\nRUN echo hi\n",
			want:    nil,
		},
		{
			name:    "docker base is current",
			project: "FROM agents-sandbox/runner-base-docker\nRUN echo hi\n",
			want:    nil,
		},
		{
			name:    "dind base warns",
			project: "FROM agents-sandbox/runner-base-dind\nRUN echo hi\n",
			want:    []string{baseWarning},
		},
		{
			name:    "dind marker warns",
			project: "FROM debian:trixie-slim\n# agents-sandbox:dind\nRUN echo hi\n",
			want:    []string{markerWarning},
		},
		{
			name:    "docker marker is current",
			project: "FROM debian:trixie-slim\n# agents-sandbox:docker\nRUN echo hi\n",
			want:    nil,
		},
		{
			name:    "both legacy forms warn",
			project: "FROM agents-sandbox/runner-base-dind\n# agents-sandbox:dind\nRUN echo hi\n",
			want:    []string{baseWarning, markerWarning},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := legacyDockerfileWarnings([]byte(tc.project))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("legacyDockerfileWarnings = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEnsureImageWarnsOnLegacyDockerfileForms verifies the warnings are emitted
// through the UI when a project Dockerfile using a legacy dind spelling is
// ensured for building.
func TestEnsureImageWarnsOnLegacyDockerfileForms(t *testing.T) {
	configpaths.WithMockConfigPaths(t)
	WithMockAgentVersion(t, "1.2.3")
	a := agentOpencode(t)

	docker.WithDockerMock(t, &docker.MockDockerClient{
		ImageInspectFn: func(_ context.Context, ref string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
			if ref == "debian:trixie-slim" {
				return client.ImageInspectResult{ID: "sha256:base"}, nil
			}
			return client.ImageInspectResult{
				ID: "sha256:existing",
				Config: &dockerspec.DockerOCIImageConfig{
					Labels: map[string]string{dockerfileIDLabelKey: "stale"},
				},
			}, nil
		},
		ImageBuildFn: func(_ context.Context, _ io.Reader, _ client.ImageBuildOptions) (client.ImageBuildResult, error) {
			return client.ImageBuildResult{Body: io.NopCloser(strings.NewReader(""))}, nil
		},
	})

	project := []byte("FROM agents-sandbox/runner-base-dind\n# agents-sandbox:dind\nRUN echo hi\n")
	ui := &termio.Mock{}
	if _, err := EnsureImageWithClient(context.Background(), a, project, "proj", BuildOptions{}, ui); err != nil {
		t.Fatalf("EnsureImageWithClient: %v", err)
	}

	joined := strings.Join(ui.WarnCalls, "\n")
	for _, want := range []string{"runner-base-dind", "# agents-sandbox:dind"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a deprecation warning mentioning %q, got %v", want, ui.WarnCalls)
		}
	}
}
