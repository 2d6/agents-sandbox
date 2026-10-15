package main

import (
	"strings"
	"testing"

	"github.com/inoio/agents-sandbox/internal/termio"
)

func TestResolveListOutput(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		quiet     bool
		wantMode  listOutput
		wantError string
	}{
		{name: "default table", format: "", quiet: false, wantMode: listOutputTable},
		{name: "quiet identifiers", format: "", quiet: true, wantMode: listOutputIdentifiers},
		{name: "json", format: formatJSON, quiet: false, wantMode: listOutputJSON},
		{name: "json and quiet conflict", format: formatJSON, quiet: true, wantError: "mutually exclusive"},
		{name: "unsupported format", format: "yaml", quiet: false, wantError: "unsupported format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := resolveListOutput(tt.format, tt.quiet)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("resolveListOutput(%q, %v) error = %v, want containing %q",
						tt.format, tt.quiet, err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveListOutput(%q, %v) unexpected error: %v", tt.format, tt.quiet, err)
			}
			if mode != tt.wantMode {
				t.Errorf("resolveListOutput(%q, %v) = %v, want %v", tt.format, tt.quiet, mode, tt.wantMode)
			}
		})
	}
}

func TestPrintListJSONEmpty(t *testing.T) {
	ui := &termio.Mock{}
	items := []string{}
	err := printList(ui, listOutputJSON, items, "none", []string{"VALUE"},
		func(items []string) any {
			out := make([]string, 0, len(items))
			return append(out, items...)
		},
		func(s string) string { return s },
	)
	if err != nil {
		t.Fatalf("printList: %v", err)
	}
	joined := strings.Join(ui.OutCalls, "\n")
	if strings.TrimSpace(joined) != "[]" {
		t.Errorf("empty JSON list should render [], got %q", joined)
	}
}
