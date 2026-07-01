package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// RunCompsInput drives the run_comps tool. Only propertyID is required — the
// comp subject (address, geo, beds/baths) is hydrated from the property's stored
// details. Any field the caller sets explicitly overrides the hydrated value.
type RunCompsInput struct {
	PropertyID string  `json:"propertyID"`
	Address    string  `json:"address,omitempty"`
	City       string  `json:"city,omitempty"`
	State      string  `json:"state,omitempty"`
	ZipCode    string  `json:"zipCode,omitempty"`
	Latitude   float64 `json:"latitude,omitempty"`
	Longitude  float64 `json:"longitude,omitempty"`
	Bedrooms   string  `json:"bedrooms,omitempty"`
	Bathrooms  string  `json:"bathrooms,omitempty"`
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

func (h *PropertyHandler) RunComps(ctx context.Context, req *mcp.CallToolRequest, input RunCompsInput) (*mcp.CallToolResult, *reisearch.RunCompsResult, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}

	// Hydrate the comp subject from the property's stored details, then let any
	// caller-supplied fields override the hydrated values.
	details, err := h.client.GetPropertyDetails(ctx, token, input.PropertyID)
	if err != nil {
		return nil, nil, err
	}
	subject := subjectFromDetails(input.PropertyID, details)

	if input.Address != "" {
		subject.Address = input.Address
	}
	if input.City != "" {
		subject.City = input.City
	}
	if input.State != "" {
		subject.State = input.State
	}
	if input.ZipCode != "" {
		subject.ZipCode = input.ZipCode
	}
	if input.Latitude != 0 {
		subject.Latitude = input.Latitude
	}
	if input.Longitude != 0 {
		subject.Longitude = input.Longitude
	}
	if input.Bedrooms != "" {
		subject.Bedrooms = input.Bedrooms
	}
	if input.Bathrooms != "" {
		subject.Bathrooms = input.Bathrooms
	}

	// Address is required by the backend; fail early with a clear message
	// instead of letting run-comps return an opaque 400.
	if subject.Address == "" {
		return nil, nil, fmt.Errorf("this property has no address on file, so comps can't be generated; add an address to the property or pass one explicitly")
	}

	result, err := h.client.RunComps(ctx, token, reisearch.RunCompsRequest{Subject: subject})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// subjectFromDetails builds a comp subject from a get_property_details payload.
// The payload wraps the property record under "property_detail"; lat/long are
// stored there as strings and are parsed to float64 for the comps API.
func subjectFromDetails(propertyID string, details map[string]interface{}) reisearch.CompSubject {
	subject := reisearch.CompSubject{PropertyID: propertyID}

	pd, _ := details["property_detail"].(map[string]interface{})
	if pd == nil {
		return subject
	}

	getString := func(key string) string {
		if v, ok := pd[key].(string); ok {
			return v
		}
		return ""
	}

	// Prefer the pre-formatted full address; otherwise compose one from parts.
	subject.Address = getString("location")
	if subject.Address == "" {
		subject.Address = composeAddress(getString("street"), getString("city"), getString("state"), getString("zipCode"))
	}
	subject.City = getString("city")
	subject.State = getString("state")
	subject.ZipCode = getString("zipCode")
	subject.Bedrooms = getString("bedrooms")
	subject.Bathrooms = getString("bathrooms")
	subject.Latitude = parseFloat(getString("lat"))
	subject.Longitude = parseFloat(getString("long"))

	return subject
}

// composeAddress joins the street line with "city, state zip", skipping blanks,
// producing e.g. "742 Evergreen Terrace, Springfield, IL 62704".
func composeAddress(street, city, state, zip string) string {
	var parts []string
	if street != "" {
		parts = append(parts, street)
	}
	if city != "" {
		parts = append(parts, city)
	}
	region := strings.TrimSpace(state + " " + zip)
	if region != "" {
		parts = append(parts, region)
	}
	return strings.Join(parts, ", ")
}

// parseFloat returns the float64 value of s, or 0 when s is empty/unparseable.
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func Register(server *mcp.Server, client *reisearch.Client) {
	h := &PropertyHandler{client: client}
	mcp.AddTool(server, &mcp.Tool{Name: "create_property", Description: "Create a new property inside ReiSearch. Requires full property address"}, h.CreateProperty)
	mcp.AddTool(server, &mcp.Tool{Name: "add_note_for_property", Description: "Adds a note to a property, requires a property id to be passed"}, h.CreateNote)
	mcp.AddTool(server, &mcp.Tool{Name: "get_notes_for_property", Description: "List notes on a property (paginated). Requires a property id; optional limit and cursor for paging."}, h.GetNotes)
	mcp.AddTool(server, &mcp.Tool{Name: "get_property_details", Description: "Get a property's full details (core info + deal structure). Requires a property id."}, h.GetPropertyDetails)
	mcp.AddTool(server, &mcp.Tool{Name: "get_property_list", Description: "List all properties the current user has access to — their property dashboard/list view, including both properties they created and ones shared with them (paginated). This is the list-view companion to get_property_details. IMPORTANT: to list a user's properties, call this with NO filters — do NOT pass 'status' or 'ownership' unless the user EXPLICITLY asks for a specific stage or ownership. Nearly all properties are in the 'draft' stage (publishing is not actively used right now), so filtering by status:'published' will almost always return an empty list — never default to it. Filters (use only on explicit user request): ownership ('mine' = created by the user, 'shared' = shared with them; omit for both); status, one of 'draft', 'published', 'archived', 'deleted' (omit for all stages). limit caps results (default 10). Pass lastKey (from a previous response) to fetch the next page."}, h.GetPropertyList)
	mcp.AddTool(server, &mcp.Tool{Name: "get_comps", Description: "Read the comparable properties ('comps') for a property under a given exit strategy. Requires propertyID and compType (the exit strategy / comparison basis, e.g. 'sold', 'rental', 'affordable_housing'). If the user hasn't said which exit strategy they want, ASK them before calling — do not guess. The result has a 'status': 'ready' (comps available), 'in_progress' (still generating — comps may already be partially populated, so show what's there and suggest checking back shortly), 'no_comps_yet' (never run for this property/strategy — offer to run comps but do NOT run them without the user's confirmation, since running comps is billed), 'no_results', 'failed', or 'cancelled'. Always relay the human-readable 'message'."}, h.GetComps)
	mcp.AddTool(server, &mcp.Tool{Name: "run_comps", Description: "Start generating comparable properties ('comps') for a property. This is BILLED (costs credits) and runs ASYNCHRONOUSLY, so ALWAYS confirm with the user before calling — the normal flow is to check get_comps first and only run when there are none. Requires only propertyID; the comp subject (address, geo coordinates, beds/baths) is pulled automatically from the property's stored details. You may optionally override address/city/state/zipCode/latitude/longitude/bedrooms/bathrooms, but this is rarely needed. One run generates comps across ALL exit strategies at once — there is no compType here. The result 'status' is one of: 'in_progress' (accepted — comps are now generating; tell the user to check back shortly with get_comps, which will show each comp as it lands), 'insufficient_credits', 'already_in_progress' (a run is already underway for this property), 'invalid_request', 'no_billing_account', or 'temporarily_unavailable'. Always relay the human-readable 'message'."}, h.RunComps)

}
