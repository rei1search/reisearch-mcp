package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

type PropertyHandler struct {
	client *reisearch.Client
}

func (h *PropertyHandler) CreateProperty(ctx context.Context, req *mcp.CallToolRequest, input reisearch.CreatePropertyRequest) (*mcp.CallToolResult, *reisearch.Property, error) {
	token := TokenFromContext(ctx)
	property, err := h.client.CreateProperty(ctx, token, input)
	if err != nil {
		return nil, nil, err
	}

	return nil, property, nil
}

func Register(server *mcp.Server, client *reisearch.Client) {
	h := &PropertyHandler{client: client}
	mcp.AddTool(server, &mcp.Tool{Name: "create_property", Description: "Create a new property inside ReiSearch. Requires full property address"}, h.CreateProperty)
}
