package adminbot

import (
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestResolveCreatedPeerNameUsesProvidedName(t *testing.T) {
	now := time.Date(2026, time.May, 3, 14, 15, 51, 123000000, time.UTC)

	got, autoNamed := resolveCreatedPeerName("  Office Router  ", now)
	if autoNamed {
		t.Fatal("expected provided peer name to be preserved")
	}
	if got != "Office Router" {
		t.Fatalf("expected trimmed peer name, got %q", got)
	}
}

func TestResolveCreatedPeerNameAutoGeneratesWhenBlank(t *testing.T) {
	now := time.Date(2026, time.May, 3, 14, 15, 51, 123000000, time.UTC)

	got, autoNamed := resolveCreatedPeerName("   ", now)
	if !autoNamed {
		t.Fatal("expected blank peer name to be auto-generated")
	}
	if got != "device-20260503-141551-123" {
		t.Fatalf("unexpected generated peer name: %q", got)
	}
}

func TestCreatePeerToolOnlyRequiresSelector(t *testing.T) {
	var createPeerTool *anthropic.ToolParam
	for _, tool := range claudeTools() {
		if tool.OfTool != nil && tool.OfTool.Name == "create_peer" {
			createPeerTool = tool.OfTool
			break
		}
	}
	if createPeerTool == nil {
		t.Fatal("create_peer tool not found")
	}

	if len(createPeerTool.InputSchema.Required) != 1 || createPeerTool.InputSchema.Required[0] != "selector" {
		t.Fatalf("expected only selector to be required, got %#v", createPeerTool.InputSchema.Required)
	}
}
