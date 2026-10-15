package main

import (
	"context"
	"testing"

	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	sandbox "github.com/inoio/agents-sandbox/internal/sandbox/vm"
	"github.com/inoio/agents-sandbox/internal/termio"

	mocks "github.com/inoio/agents-sandbox/internal/testutil/mocks"
)

func TestMain(m *testing.M) {
	// Seal the run/shell commands off from the developer's real legacy home: the
	// default migration offer is a no-op, and the tests that exercise it opt in.
	sandbox.SetRuntimeHomeMigrationOffer(func(context.Context, msb.Client, termio.UI) error { return nil })
	mocks.InitFailFastMocks(m)
}
