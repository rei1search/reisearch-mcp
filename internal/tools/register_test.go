package tools

import (
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
