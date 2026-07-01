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

type GetPropertyDetailsInput struct {
	PropertyID string `json:"propertyID"`
}

type GetPropertyListInput struct {
	Limit     int    `json:"limit,omitempty"`
	Ownership string `json:"ownership,omitempty"`
	Status    string `json:"status,omitempty"`
	LastKey   string `json:"lastKey,omitempty"`
}

type GetCompsInput struct {
	PropertyID string `json:"propertyID"`
	CompType   string `json:"compType"`
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

func (h *PropertyHandler) GetPropertyDetails(ctx context.Context, req *mcp.CallToolRequest, input GetPropertyDetailsInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)
	details, err := h.client.GetPropertyDetails(ctx, token, input.PropertyID)
	if err != nil {
		return nil, nil, err
	}
	return nil, details, nil
}

func (h *PropertyHandler) GetPropertyList(ctx context.Context, req *mcp.CallToolRequest, input GetPropertyListInput) (*mcp.CallToolResult, *reisearch.SharedPropertiesPage, error) {
	token := TokenFromContext(ctx)
	page, err := h.client.GetSharedProperties(ctx, token, input.Limit, input.Ownership, input.Status, input.LastKey)
	if err != nil {
		return nil, nil, err
	}
	return nil, page, nil
}

func (h *PropertyHandler) GetComps(ctx context.Context, req *mcp.CallToolRequest, input GetCompsInput) (*mcp.CallToolResult, *reisearch.CompsResult, error) {
	token := TokenFromContext(ctx)
	result, err := h.client.GetComps(ctx, token, input.PropertyID, input.CompType)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

func Register(server *mcp.Server, client *reisearch.Client) {
	h := &PropertyHandler{client: client}
	mcp.AddTool(server, &mcp.Tool{Name: "create_property", Description: "Create a new property inside ReiSearch. Requires full property address"}, h.CreateProperty)
	mcp.AddTool(server, &mcp.Tool{Name: "add_note_for_property", Description: "Adds a note to a property, requires a property id to be passed"}, h.CreateNote)
	mcp.AddTool(server, &mcp.Tool{Name: "get_notes_for_property", Description: "List notes on a property (paginated). Requires a property id; optional limit and cursor for paging."}, h.GetNotes)
	mcp.AddTool(server, &mcp.Tool{Name: "get_property_details", Description: "Get a property's full details (core info + deal structure). Requires a property id."}, h.GetPropertyDetails)
	mcp.AddTool(server, &mcp.Tool{Name: "get_property_list", Description: "List all properties the current user has access to — i.e. their property dashboard/list view, including both properties they created and properties shared with them (paginated). This is the list-view companion to get_property_details. Optional filters: ownership ('mine' for properties the user created, 'shared' for ones shared with them; omit for both); status to filter by stage, one of 'draft', 'published', 'archived', 'deleted' (omit for all); limit to cap results (default 10). Pass lastKey (from a previous response) to fetch the next page."}, h.GetPropertyList)
	mcp.AddTool(server, &mcp.Tool{Name: "get_comps", Description: "Read the comparable properties ('comps') for a property under a given exit strategy. Requires propertyID and compType (the exit strategy / comparison basis, e.g. 'sold', 'rental', 'affordable_housing'). If the user hasn't said which exit strategy they want, ASK them before calling — do not guess. The result has a 'status': 'ready' (comps available), 'in_progress' (still generating — comps may already be partially populated, so show what's there and suggest checking back shortly), 'no_comps_yet' (never run for this property/strategy — offer to run comps but do NOT run them without the user's confirmation, since running comps is billed), 'no_results', 'failed', or 'cancelled'. Always relay the human-readable 'message'."}, h.GetComps)

}
