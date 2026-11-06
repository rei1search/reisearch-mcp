package tools

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// TestRegisterDoesNotPanic guards the whole tool surface against the SDK's
// registration-time panics — a bare-slice output type, or an input type the
// schema reflector can't handle (e.g. map[string]any fields) — which go build
// cannot catch. Handlers are never invoked, so a zero client is fine.
func TestRegisterDoesNotPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(server, &reisearch.Client{})
}

const expectedToolCount = 76

// TestRegisteredTools lists tools over an in-memory session (what a real
// client sees) and checks the total and the underwriting tool names.
func TestRegisteredTools(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(server, &reisearch.Client{})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	names := map[string]bool{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names[tool.Name] = true
	}

	if len(names) != expectedToolCount {
		t.Errorf("registered %d tools, want %d", len(names), expectedToolCount)
	}
	for _, name := range []string{
		"list_underwriting_versions",
		"get_underwriting_section",
		"get_all_underwriting",
		"create_underwriting_section",
		"update_underwriting_section",
		"upsert_underwriting_room",
		"delete_underwriting_room",
		"clone_underwriting_version",
		"rename_underwriting_version",
		"set_underwriting_deal_score",
		"delete_underwriting_version",
		"submit_property_renovation",
		"upload_property_image",
		"remove_property_images",
		"list_property_documents",
		"get_property_document_upload_url",
		"confirm_property_document_upload",
		"update_property",
		"invite_users",
	} {
		if !names[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}
