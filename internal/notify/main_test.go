package notify

import (
	"testing"

	termio "github.com/inoio/agents-sandbox/internal/testutil/mocks"
)

func TestMain(m *testing.M) {
	InstallFailFast()
	termio.InitFailFastMocks(m)
}
