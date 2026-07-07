package tools

import (
	"context"
	"fmt"
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

// RunCompsInput drives the run_comps tool. The only input is propertyID — the
// backend loads that property and builds the comp subject (address, geo,
// beds/baths) itself.
type RunCompsInput struct {
	PropertyID string `json:"propertyID"`
}

// SearchPropertiesInput drives the search_properties tool. Every field is
// optional; the multi-value fields accept arrays and are comma-joined before
// being sent as query params. Results are always scoped server-side to the
// caller's own and shared DRAFT properties.
type SearchPropertiesInput struct {
	Address   string   `json:"address,omitempty"`
	City      string   `json:"city,omitempty"`
	ZipCode   string   `json:"zipCode,omitempty"`
	HomeTypes []string `json:"homeTypes,omitempty"`
	YearBuilt string   `json:"yearBuilt,omitempty"`
	DealTypes []string `json:"dealTypes,omitempty"`
	MinPrice  string   `json:"minPrice,omitempty"`
	MaxPrice  string   `json:"maxPrice,omitempty"`
	Beds      string   `json:"beds,omitempty"`
	ExactBed  bool     `json:"exactBed,omitempty"`
	Baths     string   `json:"baths,omitempty"`
	ExactBath bool     `json:"exactBath,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Limit     string   `json:"limit,omitempty"`
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

	// The backend loads the property and builds the comp subject itself, so the
	// only thing we send is the property id.
	result, err := h.client.RunComps(ctx, token, reisearch.RunCompsRequest{PropertyID: input.PropertyID})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// CreateFolderInput drives the create_folder tool. Name is required; ParentID
// nests the new folder under an existing one, Description is optional.
type CreateFolderInput struct {
	Name        string `json:"name"`
	ParentID    string `json:"parentID,omitempty"`
	Description string `json:"description,omitempty"`
}

// GetUnreadNotificationsInput drives the get_unread_notifications tool. Both
// fields are optional: Limit caps results (server default 20, max 100) and
// Cursor pages through results.
type GetUnreadNotificationsInput struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

func (h *PropertyHandler) CreateFolder(ctx context.Context, req *mcp.CallToolRequest, input CreateFolderInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}

	folder, err := h.client.CreateFolder(ctx, token, input.Name, input.ParentID, input.Description)
	if err != nil {
		return nil, nil, err
	}
	return nil, folder, nil
}

func (h *PropertyHandler) GetUnreadNotifications(ctx context.Context, req *mcp.CallToolRequest, input GetUnreadNotificationsInput) (*mcp.CallToolResult, *reisearch.NotificationsPage, error) {
	token := TokenFromContext(ctx)

	page, err := h.client.GetUnreadNotifications(ctx, token, input.Limit, input.Cursor)
	if err != nil {
		return nil, nil, err
	}
	return nil, page, nil
}

// SearchPropertiesOutput wraps the result array in an object. The MCP SDK
// requires a tool's structured output schema to be an object, not a bare array,
// so the matching properties are returned under a "properties" field.
type SearchPropertiesOutput struct {
	Properties []map[string]interface{} `json:"properties"`
}

func (h *PropertyHandler) SearchProperties(ctx context.Context, req *mcp.CallToolRequest, input SearchPropertiesInput) (*mcp.CallToolResult, *SearchPropertiesOutput, error) {
	token := TokenFromContext(ctx)

	// Multi-value filters go over the wire as comma-separated strings.
	params := reisearch.PropertySearchParams{
		Address:   input.Address,
		City:      input.City,
		ZipCode:   input.ZipCode,
		HomeTypes: strings.Join(input.HomeTypes, ","),
		YearBuilt: input.YearBuilt,
		DealTypes: strings.Join(input.DealTypes, ","),
		MinPrice:  input.MinPrice,
		MaxPrice:  input.MaxPrice,
		Beds:      input.Beds,
		ExactBed:  input.ExactBed,
		Baths:     input.Baths,
		ExactBath: input.ExactBath,
		Tags:      strings.Join(input.Tags, ","),
		Limit:     input.Limit,
	}

	results, err := h.client.SearchProperties(ctx, token, params)
	if err != nil {
		return nil, nil, err
	}
	return nil, &SearchPropertiesOutput{Properties: results}, nil
}

// SharePropertyInput drives the share_property tool. PropertyID and UserID are
// required; Actions is an optional custom permission list (omit to grant the
// backend defaults). Actions is a JSON-body array here, so it passes straight
// through — no comma-joining like the query-param tools.
type SharePropertyInput struct {
	PropertyID string   `json:"propertyID"`
	UserID     string   `json:"userID"`
	Actions    []string `json:"actions,omitempty"`
}

func (h *PropertyHandler) ShareProperty(ctx context.Context, req *mcp.CallToolRequest, input SharePropertyInput) (*mcp.CallToolResult, *reisearch.SharedProperty, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if input.UserID == "" {
		return nil, nil, fmt.Errorf("userID is required")
	}

	result, err := h.client.ShareProperty(ctx, token, input.PropertyID, reisearch.SharePropertyRequest{UserID: input.UserID, Actions: input.Actions})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// SearchUsersInput drives the search_users tool. Every field is optional; the
// backend infers the search mode from which fields are set (see the tool
// description). Size caps the page (default 15, max 50).
type SearchUsersInput struct {
	Name       string `json:"name,omitempty"`
	Expertise  string `json:"expertise,omitempty"`
	City       string `json:"city,omitempty"`
	Size       int    `json:"size,omitempty"`
	LastCursor string `json:"lastCursor,omitempty"`
}

func (h *PropertyHandler) SearchUsers(ctx context.Context, req *mcp.CallToolRequest, input SearchUsersInput) (*mcp.CallToolResult, *reisearch.ConnectionSearchResult, error) {
	token := TokenFromContext(ctx)

	result, err := h.client.SearchUsers(ctx, token, input.Name, input.Expertise, input.City, input.Size, input.LastCursor)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// PushToCRMInput drives the push_lead_to_crm tool. It reuses the client's
// nested CRMContact/CRMOpportunity structs (their field names already match the
// API); only the top level is redefined so propertyID matches our other tools.
type PushToCRMInput struct {
	PropertyID  string                    `json:"propertyID"`
	LocationID  string                    `json:"locationId,omitempty"`
	Contact     reisearch.CRMContact      `json:"contact"`
	Opportunity *reisearch.CRMOpportunity `json:"opportunity,omitempty"`
}

func (h *PropertyHandler) PushToCRM(ctx context.Context, req *mcp.CallToolRequest, input PushToCRMInput) (*mcp.CallToolResult, *reisearch.CRMPushResult, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if input.Contact.ContactType == "" {
		return nil, nil, fmt.Errorf("contact.contactType is required")
	}

	result, err := h.client.PushToCRM(ctx, token, reisearch.CRMPushRequest{
		PropertyID:  input.PropertyID,
		LocationID:  input.LocationID,
		Contact:     input.Contact,
		Opportunity: input.Opportunity,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// CRMConnectionsInput is empty: get_crm_connections takes no parameters (it
// lists every connected CRM account).
type CRMConnectionsInput struct{}

func (h *PropertyHandler) GetCRMConnections(ctx context.Context, req *mcp.CallToolRequest, input CRMConnectionsInput) (*mcp.CallToolResult, *reisearch.CRMConnectionsResult, error) {
	token := TokenFromContext(ctx)
	result, err := h.client.GetCRMConnections(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// CRMLocationInput is the shared input for the per-location CRM pickers. Pass
// locationId to target a specific connected CRM; omit it when exactly one CRM
// is connected.
type CRMLocationInput struct {
	LocationID string `json:"locationId,omitempty"`
}

func (h *PropertyHandler) GetCRMPipelines(ctx context.Context, req *mcp.CallToolRequest, input CRMLocationInput) (*mcp.CallToolResult, *reisearch.CRMPipelinesResult, error) {
	token := TokenFromContext(ctx)
	result, err := h.client.GetCRMPipelines(ctx, token, input.LocationID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

func (h *PropertyHandler) GetCRMUsers(ctx context.Context, req *mcp.CallToolRequest, input CRMLocationInput) (*mcp.CallToolResult, *reisearch.CRMUsersResult, error) {
	token := TokenFromContext(ctx)
	result, err := h.client.GetCRMUsers(ctx, token, input.LocationID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

func (h *PropertyHandler) GetCRMTags(ctx context.Context, req *mcp.CallToolRequest, input CRMLocationInput) (*mcp.CallToolResult, *reisearch.CRMTagsResult, error) {
	token := TokenFromContext(ctx)
	result, err := h.client.GetCRMTags(ctx, token, input.LocationID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// CreateCRMOpportunityInput drives the create_crm_opportunity tool. All fields
// but locationId are required.
type CreateCRMOpportunityInput struct {
	LocationID string `json:"locationId,omitempty"`
	PipelineID string `json:"pipelineId"`
	ContactID  string `json:"contactId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

func (h *PropertyHandler) CreateCRMOpportunity(ctx context.Context, req *mcp.CallToolRequest, input CreateCRMOpportunityInput) (*mcp.CallToolResult, *reisearch.CRMOpportunityResult, error) {
	token := TokenFromContext(ctx)

	if input.PipelineID == "" {
		return nil, nil, fmt.Errorf("pipelineId is required")
	}
	if input.ContactID == "" {
		return nil, nil, fmt.Errorf("contactId is required")
	}
	if input.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if input.Status == "" {
		return nil, nil, fmt.Errorf("status is required")
	}

	result, err := h.client.CreateCRMOpportunity(ctx, token, reisearch.CRMCreateOpportunityRequest{
		LocationID: input.LocationID,
		PipelineID: input.PipelineID,
		ContactID:  input.ContactID,
		Name:       input.Name,
		Status:     input.Status,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// AddCRMNoteInput drives the add_crm_note tool. PropertyID and Note are
// required; LocationID is optional (needed only when several CRMs are connected).
type AddCRMNoteInput struct {
	PropertyID string `json:"propertyID"`
	Note       string `json:"note"`
	LocationID string `json:"locationId,omitempty"`
}

func (h *PropertyHandler) AddCRMNote(ctx context.Context, req *mcp.CallToolRequest, input AddCRMNoteInput) (*mcp.CallToolResult, *reisearch.CRMAddNoteResult, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if input.Note == "" {
		return nil, nil, fmt.Errorf("note is required")
	}

	result, err := h.client.AddCRMNote(ctx, token, input.PropertyID, input.Note, input.LocationID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// ---------------------------------------------------------------------------
// Folder tools
// ---------------------------------------------------------------------------

// GetFolderInfoInput drives get_folder_info. FolderID is required.
type GetFolderInfoInput struct {
	FolderID string `json:"folderID"`
}

func (h *PropertyHandler) GetFolderInfo(ctx context.Context, req *mcp.CallToolRequest, input GetFolderInfoInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.FolderID == "" {
		return nil, nil, fmt.Errorf("folderID is required")
	}

	info, err := h.client.GetFolderInfo(ctx, token, input.FolderID)
	if err != nil {
		return nil, nil, err
	}
	return nil, info, nil
}

// ListFoldersInput drives list_my_folders. All fields are optional: Limit caps
// the page, LastKey pages through results, and FolderID drills into a specific
// folder's contents instead of listing top-level folders.
type ListFoldersInput struct {
	Limit    int    `json:"limit,omitempty"`
	LastKey  string `json:"lastKey,omitempty"`
	FolderID string `json:"folderID,omitempty"`
}

func (h *PropertyHandler) ListFolders(ctx context.Context, req *mcp.CallToolRequest, input ListFoldersInput) (*mcp.CallToolResult, *reisearch.FolderListPage, error) {
	token := TokenFromContext(ctx)

	page, err := h.client.ListFolders(ctx, token, input.Limit, input.LastKey, input.FolderID)
	if err != nil {
		return nil, nil, err
	}
	return nil, page, nil
}

// GetFolderMembersInput drives get_folder_members. FolderID is required.
type GetFolderMembersInput struct {
	FolderID string `json:"folderID"`
}

func (h *PropertyHandler) GetFolderMembers(ctx context.Context, req *mcp.CallToolRequest, input GetFolderMembersInput) (*mcp.CallToolResult, *reisearch.FolderMembersResult, error) {
	token := TokenFromContext(ctx)

	if input.FolderID == "" {
		return nil, nil, fmt.Errorf("folderID is required")
	}

	result, err := h.client.GetFolderMembers(ctx, token, input.FolderID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// AddPropertyToFolderInput drives add_property_to_folder. FolderID, PropertyID,
// and Mode ("move" or "copy") are required. PreviousFolderID enables clean-up on
// a move; the copy flags are only meaningful when Mode=="copy".
type AddPropertyToFolderInput struct {
	FolderID          string `json:"folderID"`
	PropertyID        string `json:"propertyID"`
	Mode              string `json:"mode"`
	PreviousFolderID  string `json:"previousFolderID,omitempty"`
	CopyDealStructure bool   `json:"copyDealStructure,omitempty"`
	CopyDocuments     bool   `json:"copyDocuments,omitempty"`
	CopyComps         bool   `json:"copyComps,omitempty"`
}

func (h *PropertyHandler) AddPropertyToFolder(ctx context.Context, req *mcp.CallToolRequest, input AddPropertyToFolderInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.FolderID == "" {
		return nil, nil, fmt.Errorf("folderID is required")
	}
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if input.Mode != "move" && input.Mode != "copy" {
		return nil, nil, fmt.Errorf("mode is required and must be 'move' or 'copy'")
	}

	result, err := h.client.AddPropertyToFolder(ctx, token, reisearch.AddPropertyToFolderRequest{
		FolderID:          input.FolderID,
		PropertyID:        input.PropertyID,
		Mode:              input.Mode,
		PreviousFolderID:  input.PreviousFolderID,
		CopyDealStructure: input.CopyDealStructure,
		CopyDocuments:     input.CopyDocuments,
		CopyComps:         input.CopyComps,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// AddFolderMemberInput drives add_folder_member. FolderID and MemberID are
// required; ExistingPropertyAccess grants the member access to properties
// already in the folder.
type AddFolderMemberInput struct {
	FolderID               string `json:"folderID"`
	MemberID               string `json:"memberID"`
	ExistingPropertyAccess bool   `json:"existingPropertyAccess,omitempty"`
}

func (h *PropertyHandler) AddFolderMember(ctx context.Context, req *mcp.CallToolRequest, input AddFolderMemberInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.FolderID == "" {
		return nil, nil, fmt.Errorf("folderID is required")
	}
	if input.MemberID == "" {
		return nil, nil, fmt.Errorf("memberID is required")
	}

	result, err := h.client.AddFolderMember(ctx, token, input.FolderID, input.MemberID, input.ExistingPropertyAccess)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// MoveFolderInput drives move_folder. Both fields are required.
type MoveFolderInput struct {
	MovingFolderID string `json:"movingFolderID"`
	TargetFolderID string `json:"targetFolderID"`
}

func (h *PropertyHandler) MoveFolder(ctx context.Context, req *mcp.CallToolRequest, input MoveFolderInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.MovingFolderID == "" {
		return nil, nil, fmt.Errorf("movingFolderID is required")
	}
	if input.TargetFolderID == "" {
		return nil, nil, fmt.Errorf("targetFolderID is required")
	}

	result, err := h.client.MoveFolder(ctx, token, reisearch.MoveFolderRequest{
		MovingFolderID: input.MovingFolderID,
		TargetFolderID: input.TargetFolderID,
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// RenameFolderInput drives rename_folder. Both fields are required.
type RenameFolderInput struct {
	FolderID string `json:"folderID"`
	Name     string `json:"name"`
}

func (h *PropertyHandler) RenameFolder(ctx context.Context, req *mcp.CallToolRequest, input RenameFolderInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.FolderID == "" {
		return nil, nil, fmt.Errorf("folderID is required")
	}
	if input.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}

	result, err := h.client.RenameFolder(ctx, token, input.FolderID, input.Name)
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
	mcp.AddTool(server, &mcp.Tool{Name: "get_property_list", Description: "List all properties the current user has access to — their property dashboard/list view, including both properties they created and ones shared with them (paginated). This is the list-view companion to get_property_details. IMPORTANT: to list a user's properties, call this with NO filters — do NOT pass 'status' or 'ownership' unless the user EXPLICITLY asks for a specific stage or ownership. Nearly all properties are in the 'draft' stage (publishing is not actively used right now), so filtering by status:'published' will almost always return an empty list — never default to it. Filters (use only on explicit user request): ownership ('mine' = created by the user, 'shared' = shared with them; omit for both); status, one of 'draft', 'published', 'archived', 'deleted' (omit for all stages). limit caps results (default 10). Pass lastKey (from a previous response) to fetch the next page."}, h.GetPropertyList)
	mcp.AddTool(server, &mcp.Tool{Name: "get_comps", Description: "READ the already-generated comparable properties ('comps') for a property under a given exit strategy. This is a read-only tool — it does NOT run/generate comps and is never billed; use run_comps to generate. Requires propertyID and compType (the exit strategy / comparison basis, e.g. 'sold', 'rental', 'affordable_housing'). The compType requirement applies ONLY to reading results here — do NOT ask for or require an exit strategy when RUNNING comps with run_comps. If the user hasn't said which exit strategy they want to READ, ASK them before calling this tool — do not guess. The result has a 'status': 'ready' (comps available), 'in_progress' (still generating — comps may already be partially populated, so show what's there and suggest checking back shortly), 'no_comps_yet' (never run for this property/strategy — offer to run comps but do NOT run them without the user's confirmation, since running comps is billed), 'no_results', 'failed', or 'cancelled'. Always relay the human-readable 'message'."}, h.GetComps)
	mcp.AddTool(server, &mcp.Tool{Name: "run_comps", Description: "Start generating comparable properties ('comps') for a property. IMPORTANT: running comps does NOT take a comp type / exit strategy. Do NOT ask the user to choose 'sold', 'rental', 'affordable_housing', or any strategy before running — a single run generates comps across ALL exit strategies at once. (Choosing an exit strategy is only relevant later, when READING results with get_comps.) This is BILLED (costs credits) and runs ASYNCHRONOUSLY, so the only thing to confirm before calling is that the user is OK spending credits to run; the normal flow is to check get_comps first and only run when there are none. Requires ONLY propertyID; the backend loads the property and builds the comp subject (address, geo coordinates, beds/baths) itself — there are no other parameters to pass. The result 'status' is one of: 'in_progress' (accepted — comps are now generating; tell the user to check back shortly with get_comps, which will show each comp as it lands), 'insufficient_credits', 'already_in_progress' (a run is already underway for this property), 'invalid_request', 'no_billing_account', or 'temporarily_unavailable'. A 'property_not_found' outcome means no property exists for that propertyID — create the property first, then run comps. Always relay the human-readable 'message'."}, h.RunComps)
	mcp.AddTool(server, &mcp.Tool{Name: "search_properties", Description: "Search the current user's OWN and SHARED properties by location and filters — their personal property database. This is a filtered search over the user's existing properties, NOT a search of homes for sale on the open market. Results are ALWAYS scoped server-side to properties the user owns or that are shared with them, and only those in the 'draft' stage; this cannot be changed from the request. All filters are optional — with none supplied it returns the caller's draft properties, most recently updated first. Filters: 'address' (free-text location; fuzzy-matches street/address/number/zip and, when set, orders results by relevance instead of date); 'city' (exact, case-insensitive); 'zipCode' (exact); 'homeTypes' (array of property types, e.g. ['house','condo']; matches ANY); 'dealTypes' (array of deal types; matches ANY); 'tags' (array of the user's property tag names; matches ANY — note: if none of the names resolve to a tag this user actually has, the result is EMPTY, not unfiltered); 'yearBuilt' (returns properties built in or after this year); 'minPrice'/'maxPrice' (listing price range); 'beds' and 'baths' (by default 'that many or more' — set 'exactBed'/'exactBath' to true to require an exact match); 'limit' (max results, default 50). Numeric fields are passed as strings (e.g. beds:'3', minPrice:'100000'); an unparseable value silently skips just that one filter. Returns a JSON array of property objects (empty array [] when nothing matches), each including its id and indexed fields (street, city, zipCode, listingPrice, bedrooms, bathrooms, propertyType, propertyTags, images, etc.)."}, h.SearchProperties)
	mcp.AddTool(server, &mcp.Tool{Name: "create_folder", Description: "Create a folder in the current user's workspace to organize properties. Requires 'name' (1–100 characters). Optionally pass 'parentID' to nest the new folder inside an existing folder (omit to create a top-level/root folder), and 'description' for a short note. Returns the created folder object (including its id). Note: this creates a new folder every time it's called, so confirm the name with the user rather than guessing, and don't call it repeatedly for the same request."}, h.CreateFolder)
	mcp.AddTool(server, &mcp.Tool{Name: "get_unread_notifications", Description: "List the current user's unread, active (non-dismissed) notifications, most recent first, paginated. Both parameters are optional: 'limit' caps how many are returned (defaults to 20 server-side, max 100) and 'cursor' fetches the next page (pass the 'nextCursor' from a previous response). Returns an object with 'items' (each notification includes notificationid, type, detail, actions, created, is_read, is_dismissed, and property_id when the notification is about a property) and, when more results exist, 'nextCursor'. An empty 'items' list means the user has no unread notifications."}, h.GetUnreadNotifications)
	mcp.AddTool(server, &mcp.Tool{Name: "share_property", Description: "Share a property/deal with another user: adds them as a collaborator, grants permissions, and sends them a 'property_shared' notification. Requires 'propertyID' (a valid UUID or ULID) and 'userID' (the user to add — this cannot be the property's owner, who already has full access). The caller must have permission to add users to this deal. 'actions' is OPTIONAL: omit it to grant the sensible defaults (view, edit, and the ability to add other collaborators). A custom 'actions' list is only honored when the caller owns the property or can manage its permissions — otherwise the defaults are granted no matter what is sent; 'property:View' is always included; and only the owner can grant 'property:ManagePermissions'. If the target user is already a collaborator the call fails (they can't be added twice); otherwise new actions are merged with any they already have, never removed. Valid actions: property:View, property:Edit, property:Delete, property:ViewAddress, property:ViewAddressRequest, property:AcceptAddressRequest, property:DeclineAddressRequest, property:ViewOffers, property:AcceptOffers, property:AddUserToDeal, property:CreateThread, property:ViewThreads, property:ManagePermissions, property:AddToFolder. Returns the share record on success; a failure (already shared, target is the owner, caller lacks permission, or property not found) is surfaced as an error."}, h.ShareProperty)
	mcp.AddTool(server, &mcp.Tool{Name: "search_users", Description: "Search the CURRENT user's CONNECTIONS — the people in their own network — e.g. to find the userID to pass to share_property. This searches ONLY the caller's connections, never all users globally and never properties; someone who is not in the caller's network will not appear, so an empty result means 'no matching connections'. All parameters are optional and the backend infers the search MODE from what you send: send NONE of name/expertise/city to LIST the full connection list (paginate with lastCursor); send 'name' to search by name; send 'expertise' and/or 'city' to filter by those attributes. 'size' is the max results per page (default 15, max 50). IMPORTANT: cursor pagination with 'lastCursor' works ONLY in LIST mode — when searching by name or filtering there is no cursor paging, so raise 'size' (up to 50) to see more matches. In LIST mode, pass the 'next_cursor' from the previous response as 'lastCursor' to get the next page; an empty 'next_cursor' means there are no more pages. Returns an object with 'connections' (each entry has the connection's 'user' profile — including the 'id' you pass as userID to share_property — plus relationship 'status' and 'direction'), 'total' (the count in THIS page, not a global total), 'size' (the page size applied), and 'next_cursor'."}, h.SearchUsers)
	mcp.AddTool(server, &mcp.Tool{Name: "push_lead_to_crm", Description: "Push a ReiSearch property to the user's connected CRM (GoHighLevel) as a new contact. You supply 'propertyID' and a 'contact' block; the property's address, deal-structure numbers, and stored owner/agent/mailing info are mapped onto the CRM's custom fields automatically. Optionally also create an 'opportunity' in a pipeline and attach 'notes' in the same call. PREREQUISITE: the user must have already connected a CRM in the ReiSearch app — this tool cannot connect one. Call get_crm_connections first to get the 'locationId' (OPTIONAL when exactly one CRM is connected, REQUIRED when several are) and to confirm a CRM exists at all; use get_crm_pipelines for 'opportunity.pipelineId', get_crm_users for 'contact.assignedTo', and get_crm_tags for 'contact.tags'. CONTACT: 'contact.contactType' is REQUIRED — one of property_owner, agent, jv_partner, bird_dog, buyer. 'contact.useStoredData' defaults to true: for property_owner/agent the identity is drawn from the property's stored data (first owner / listing agent) and falls back to the fields you pass; for jv_partner/bird_dog/buyer there is no stored source, so you must supply at least one of firstName/lastName/email/phone. OPPORTUNITY (optional): if you include it, pipelineId, name, and status are all required, and status must be one of open, won, lost, abandoned. IMPORTANT — non-fatal opportunity failure: a successful push can still return opportunity.created=false with an 'error'; the contact WAS created, so relay that the opportunity failed and offer to retry it with create_crm_opportunity (do NOT re-push the contact). DUPLICATES: pushing the same property to the same CRM location twice fails with 'ALREADY_IN_CRM' — treat that as a benign 'already pushed', not a hard error. ALWAYS check the returned 'status': 'created' means the contact was made (result includes contactId, locationId, propertyId, contactSource='property'|'request', and the opportunity outcome when one was requested). Any other status is a non-fatal OUTCOME to relay to the user, not a crash: 'already_in_crm' (benign — this property was already pushed to this CRM; nothing was changed), 'crm_rejected' (the CRM refused it — check details.reason, e.g. duplicate_email / duplicate_phone, plus details.crmMessage), 'location_required' (multiple CRMs connected — see details.connectedLocationIds and retry with a locationId), 'crm_reconnect_required' (the user must reconnect their CRM in the ReiSearch app), 'crm_not_connected', 'contact_info_required' (no identity available — see details.storedDataAvailable), or 'invalid_opportunity'/'invalid_opportunity_status'. Always relay the human-readable 'message'."}, h.PushToCRM)
	mcp.AddTool(server, &mcp.Tool{Name: "get_crm_connections", Description: "List the CRM accounts (GoHighLevel 'locations') the user has connected to ReiSearch. Takes NO parameters. Returns 'connections' (each with locationId, accountName, connectedAt) and 'total'. Call this before push_lead_to_crm to get the 'locationId' and to confirm a CRM is connected: if 'total' is 0 the user has not connected a CRM (they must do that in the ReiSearch app — it cannot be done through these tools). When exactly one CRM is connected, 'locationId' is optional on the other CRM tools; when several are connected you must pass the chosen locationId."}, h.GetCRMConnections)
	mcp.AddTool(server, &mcp.Tool{Name: "get_crm_pipelines", Description: "List the pipelines (and their stages) in a connected CRM location — use this to get the 'pipelineId' for an opportunity (in push_lead_to_crm or create_crm_opportunity). Optional 'locationId' (required only when several CRMs are connected; omit it when exactly one is). Returns 'locationId', 'pipelines' (each with id, name, and stages [{id, name, position}]), and 'total'."}, h.GetCRMPipelines)
	mcp.AddTool(server, &mcp.Tool{Name: "get_crm_users", Description: "List the CRM users in a connected CRM location — use this to get the user 'id' to pass as 'contact.assignedTo' when pushing a lead. Optional 'locationId' (required only when several CRMs are connected). Returns 'locationId', 'users' (each with id, name, email), and 'total'."}, h.GetCRMUsers)
	mcp.AddTool(server, &mcp.Tool{Name: "get_crm_tags", Description: "List the tag names available in a connected CRM location — use this to pick existing 'contact.tags' when pushing a lead (you may also pass brand-new tag names). Optional 'locationId' (required only when several CRMs are connected). Returns 'locationId', 'tags' (an array of tag name strings), and 'total'."}, h.GetCRMTags)
	mcp.AddTool(server, &mcp.Tool{Name: "create_crm_opportunity", Description: "Create an opportunity in a CRM pipeline for a contact that ALREADY exists in the CRM. The main use is to RETRY an opportunity after push_lead_to_crm reported a non-fatal failure (opportunity.created=false) — do NOT re-run push_lead_to_crm for that, because the contact already exists. Requires 'contactId' (the CRM contact id, e.g. the contactId returned by a previous push), 'pipelineId' (from get_crm_pipelines), 'name', and 'status' (one of open, won, lost, abandoned). 'locationId' is optional (required only when several CRMs are connected). Returns opportunityId plus the echoed locationId, pipelineId, contactId, name, and status."}, h.CreateCRMOpportunity)
	mcp.AddTool(server, &mcp.Tool{Name: "add_crm_note", Description: "Add a note to the CRM contact created by a previous push of a property (push_lead_to_crm). Requires 'propertyID' (the ReiSearch property that was pushed) and 'note' (the note text). 'locationId' is optional (required only when several CRMs are connected). If the property was never pushed to the CRM this fails with 'PROPERTY_NOT_IN_CRM' — push it first with push_lead_to_crm. Returns propertyId, contactId, and locationId."}, h.AddCRMNote)

	mcp.AddTool(server, &mcp.Tool{Name: "get_folder_info", Description: "Get a single folder's aggregate info in one call: its metadata plus the properties, members (users), and subfolders it contains, along with counts. Requires 'folderID' (a valid UUID or ULID). Use this to 'open' a folder and see what's inside — it's the detail companion to list_my_folders. Returns folder_id, folder_name, created_at, created_by, subfolder_count and subfolders (ids), property_count and properties (ids), and user_count and users (ids). The caller needs folder:View permission on the folder."}, h.GetFolderInfo)
	mcp.AddTool(server, &mcp.Tool{Name: "list_my_folders", Description: "List the current user's folders (paginated) — their folder/workspace view. All parameters are optional: 'limit' caps the page, 'lastKey' fetches the next page (pass the 'lastKey' from a previous response; an empty value means no more pages). IMPORTANT: passing 'folderID' changes the behavior — instead of listing top-level folders it DRILLS INTO that folder and returns its contents (subfolders/properties), which has a different shape. Omit 'folderID' to list the user's folders. Without 'folderID' the response has 'folders' (an array of folder records, each with its id and name), 'lastKey', and 'count'. Use get_folder_info for a single folder's full details and members."}, h.ListFolders)
	mcp.AddTool(server, &mcp.Tool{Name: "get_folder_members", Description: "List the collaborators (members) of a folder. Requires 'folderID' (a valid UUID or ULID); the caller needs folder:View permission on it. Returns 'members' — an array of collaboration records, each identifying a user and their access. Use this to see who a folder is shared with (e.g. before add_folder_member) or to get a member id to remove."}, h.GetFolderMembers)
	mcp.AddTool(server, &mcp.Tool{Name: "add_property_to_folder", Description: "Link a property into a folder, either by MOVING it (re-link, so it leaves its previous folder) or COPYING it (clone into the folder, leaving the original in place). Requires 'folderID' (destination), 'propertyID' (the property to link), and 'mode' — exactly one of 'move' or 'copy'. When moving, pass 'previousFolderID' so the old link is cleaned up. When 'mode' is 'copy' you may also set 'copyDealStructure', 'copyDocuments', and/or 'copyComps' to true to clone those parts of the property (they are ignored for a move). The caller needs folder:ResourceManagement on the destination folder. Fails if the property is already in the folder. Returns the new folder-property link (a copy also includes the OriginalPropertyID)."}, h.AddPropertyToFolder)
	mcp.AddTool(server, &mcp.Tool{Name: "add_folder_member", Description: "Add a single member (collaborator) to a folder, sharing the folder with them. Requires 'folderID' and 'memberID' (both valid UUID or ULID; 'memberID' is the user to add — use search_users to find their id). Optionally set 'existingPropertyAccess' to true to also grant the new member access to the properties ALREADY in the folder (default false = they only get access to properties added afterward). The caller needs folder:ResourceManagement on the folder. Fails if the user is already a member. Returns a confirmation message."}, h.AddFolderMember)
	mcp.AddTool(server, &mcp.Tool{Name: "move_folder", Description: "Move a folder (together with its entire subtree of subfolders) under a new parent folder. Requires 'movingFolderID' (the folder to move) and 'targetFolderID' (the new parent), both valid UUID or ULID. Permission is enforced by the backend. This fails with a clear message when the move is invalid: moving a folder into itself, into one of its own descendants (cycle), exceeding the maximum nesting depth, or when the subtree is too large to move atomically. A no-op move (already a child of the target) succeeds without changes. Returns the updated folder and a summary of affected descendants."}, h.MoveFolder)
	mcp.AddTool(server, &mcp.Tool{Name: "rename_folder", Description: "Rename a folder. Requires 'folderID' (a valid UUID or ULID) and the new 'name'. The caller needs folder:Edit permission on the folder. Returns a confirmation message. Confirm the new name with the user rather than guessing."}, h.RenameFolder)

}
