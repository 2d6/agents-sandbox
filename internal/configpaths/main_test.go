package configpaths

import (
	"testing"

	"github.com/inoio/agents-sandbox/internal/sandbox/docker"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
)

func TestMain(m *testing.M) {
	// can't call testutil.InitFailFastMocks(m) because of cyclic dependency. Keep in sync with
	// testutil.InitFailFastMocks' body, except doctor.InstallFailFast() which is intentionally
	// omitted: configpaths never invokes doctor, and importing it now creates a
	// configpaths -> doctor -> runtime -> configpaths test cycle.
	InstallFailFastConfigPaths()
	docker.InstallFailFastGet()
	msb.InstallFailFastGet()
	m.Run()
}
