package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

type PropertyHandler struct {
	client *reisearch.Client
}

type CreateNoteInput struct {
	PropertyID       string   `json:"propertyID"`
	Text             string   `json:"text"`
	MentionedUserIDs []string `json:"mentioned_userIDs,omitempty"`
}

type GetNotesInput struct {
	PropertyID string `json:"propertyID"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}

func (h *PropertyHandler) CreateProperty(ctx context.Context, req *mcp.CallToolRequest, input reisearch.CreatePropertyRequest) (*mcp.CallToolResult, *reisearch.Property, error) {
	token := TokenFromContext(ctx)
	property, err := h.client.CreateProperty(ctx, token, input)
	if err != nil {
		return nil, nil, err
	}

	return nil, property, nil
}

func (h *PropertyHandler) CreateNote(ctx context.Context, req *mcp.CallToolRequest, input CreateNoteInput) (*mcp.CallToolResult, *reisearch.Note, error) {
	// 1. token from context
	token := TokenFromContext(ctx)
	// 2. call h.client.CreateNote(ctx, token, input.PropertyID, reisearch.CreateNoteRequest{ ...map Text + MentionedUserIDs... })
	note, err := h.client.CreateNote(ctx, token, input.PropertyID, reisearch.CreateNoteRequest{Text: input.Text, MentionedUserIDs: input.MentionedUserIDs})
	// 3. handle err; return (nil, note, nil)
	if err != nil {
		return nil, nil, err
	}
	return nil, note, nil
}

func (h *PropertyHandler) GetNotes(ctx context.Context, req *mcp.CallToolRequest, input GetNotesInput) (*mcp.CallToolResult, *reisearch.NotesPage, error) {
	token := TokenFromContext(ctx)
	page, err := h.client.GetNotes(ctx, token, input.PropertyID, input.Cursor, input.Limit)
	if err != nil {
		return nil, nil, err
	}
	return nil, page, nil
}

func Register(server *mcp.Server, client *reisearch.Client) {
	h := &PropertyHandler{client: client}
	mcp.AddTool(server, &mcp.Tool{Name: "create_property", Description: "Create a new property inside ReiSearch. Requires full property address"}, h.CreateProperty)
	mcp.AddTool(server, &mcp.Tool{Name: "add_note_for_property", Description: "Adds a note to a property, requires a property id to be passed"}, h.CreateNote)
	mcp.AddTool(server, &mcp.Tool{Name: "get_notes_for_property", Description: "List notes on a property (paginated). Requires a property id; optional limit and cursor for paging."}, h.GetNotes)

}
