package reisearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type CreatePropertyRequest struct {
	DealType          string  `json:"dealType"` // required
	Location          string  `json:"location"` // required
	Street            string  `json:"street,omitempty"`
	City              string  `json:"city,omitempty"`
	State             string  `json:"state,omitempty"`
	ZipCode           string  `json:"zipCode,omitempty"`
	CreativeStructure string  `json:"creativeStructure,omitempty"`
	PropertyType      string  `json:"propertyType,omitempty"`
	ListingPrice      float64 `json:"listingPrice,omitempty"`
	Lat               string  `json:"lat,omitempty"`
	Long              string  `json:"long,omitempty"`
}

type Property struct {
	PropertyID        string  `json:"propertyID"`
	Street            string  `json:"street"`
	City              string  `json:"city"`
	State             string  `json:"state"`
	ZipCode           string  `json:"zipCode"`
	DealType          string  `json:"dealType"`
	CreativeStructure string  `json:"creativeStructure"`
	ListingPrice      float64 `json:"listingPrice"`
	PropertyType      string  `json:"propertyType"`
	Location          string  `json:"location"`
	CreatedAt         int64   `json:"createdAt"`
	UserID            string  `json:"userID"`
}

type createPropertyResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    Property `json:"data"`
}

type CreateNoteRequest struct {
	Text             string   `json:"text"`
	MentionedUserIDs []string `json:"mentioned_userIDs"`
}

type createNoteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    Note   `json:"data"`
}
type Note struct {
	Id               string   `json:"id"`
	UserID           string   `json:"userID"`
	PropertyID       string   `json:"propertyID"`
	Text             string   `json:"text"`
	MentionedUserIDs []string `json:"mentioned_userIDs"`
	CreatedAt        int64    `json:"createdAt"`
	UpdatedAt        int64    `json:"updatedAt"`
}

// NotesPage is the paginated "data" payload the notes list endpoint returns:
// a batch of notes plus an opaque cursor for the next page (empty when there
// are no more).
type NotesPage struct {
	Notes     []Note `json:"notes"`
	NextToken string `json:"next_token"`
}

type getNotesResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Data    NotesPage `json:"data"`
}

// SharePropertyRequest is the POST /property/{id}/share body. UserID is the
// collaborator to add; Actions is an optional custom permission list — omit it
// to let the backend grant the defaults (property:View, property:Edit,
// property:AddUserToDeal).
type SharePropertyRequest struct {
	UserID  string   `json:"userID"`
	Actions []string `json:"actions,omitempty"`
}

// SharedProperty is the share record returned on success. The backend also
// emits internal DynamoDB sort keys (statusSorted, etc.); we drop them here
// since they aren't useful to the caller.
type SharedProperty struct {
	ID         string `json:"id"`
	OwnerID    string `json:"ownerId"`
	UserID     string `json:"userId"`
	PropertyID string `json:"propertyId"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type sharePropertyResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    SharedProperty `json:"data"`
}

// SharedPropertiesPage is the tool-output shape for the property list endpoint:
// a batch of shared-property records (each carrying its full nested property)
// plus an opaque cursor for the next page (empty when there are no more). Each
// item is passed through as a generic map rather than mirroring the large,
// deeply-nested backend property model.
type SharedPropertiesPage struct {
	Properties []map[string]interface{} `json:"properties"`
	LastKey    string                   `json:"lastKey"`
}

// getSharedPropertiesResponse mirrors the backend's actual envelope, which
// double-nests: the standard {success, message, data} wrapper, whose "data" is
// itself {"data": [...items...], "lastKey": "<cursor>"}.
type getSharedPropertiesResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Data    []map[string]interface{} `json:"data"`
		LastKey string                   `json:"lastKey"`
	} `json:"data"`
}

// CompsResult is the tool-output shape for the comps read endpoint. The read
// always succeeds at the HTTP level; Status ("ready", "in_progress",
// "no_comps_yet", "no_results", "failed", "cancelled") and Message describe the
// actual state. Comps may already carry results even while Status is
// "in_progress", because workers save each comp as it finishes. Each comp is
// passed through as a generic map (the backend emits a dynamic near-full
// passthrough of its comp model minus internal-only fields).
type CompsResult struct {
	Status  string                   `json:"status"`
	Message string                   `json:"message"`
	Comps   []map[string]interface{} `json:"comps"`
}

type getCompsResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    CompsResult `json:"data"`
}

// RunCompsRequest is the POST /run-comps body. The only input is the property
// id — the backend loads that property and builds the comp subject (address,
// geo, beds/baths) itself. One run covers all exit strategies — there is no
// compType.
type RunCompsRequest struct {
	PropertyID string `json:"propertyId"`
}

// RunCompsResult is the tool-output shape for the run endpoint. Running comps is
// billed and asynchronous. The backend returns different HTTP codes per billing
// outcome (202/400/402/404/409/502) but always the same {status,message} body,
// so we normalize every modeled outcome into this struct. Status is one of
// "in_progress", "insufficient_credits", "already_in_progress",
// "invalid_request", "no_billing_account", or "temporarily_unavailable".
type RunCompsResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// runCompsEnvelope captures both placements of the RunCompsResult: `data` on
// success (202) and `error.details` for the modeled billing outcomes. For
// PROPERTY_NOT_FOUND (404) there is no `details`, so we fall back to error.code
// and error.message.
type runCompsEnvelope struct {
	Data  *RunCompsResult `json:"data"`
	Error *struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details *RunCompsResult `json:"details"`
	} `json:"error"`
}

func NewClient(baseUrl string) *Client {
	return &Client{
		baseURL:    baseUrl,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) CreateNote(ctx context.Context, token, propertyID string, req CreateNoteRequest) (*Note, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	requrl := c.baseURL + "/connect/v1/property/" + url.PathEscape(propertyID) + "/note/"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	//sending the api request

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create note for property failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// decode the envelopes and return the inner property
	var parsed createNoteResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil

}

// ShareProperty adds a user as a collaborator on a property and grants them
// permissions; the backend also sends the added user a property_shared
// notification. Success is HTTP 200 with the share record. Every modeled
// failure (already shared, target is the owner, no permission, property not
// found) comes back as a non-200 with the standard error envelope, which we
// surface as a Go error carrying the response body.
func (c *Client) ShareProperty(ctx context.Context, token, propertyID string, req SharePropertyRequest) (*SharedProperty, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	requrl := c.baseURL + "/connect/v1/property/" + url.PathEscape(propertyID) + "/share"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("share property failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed sharePropertyResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

func (c *Client) GetNotes(ctx context.Context, token, propertyID, cursor string, limit int) (*NotesPage, error) {
	// Only send params the caller actually provided; the backend defaults
	// limit to 10 on its own, so an omitted limit means "use the default"
	// rather than "return 0 notes".
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}

	requrl := c.baseURL + "/connect/v1/property/" + url.PathEscape(propertyID) + "/note/"
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	// GET has no body.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get notes for property failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// decode the envelope and return the inner page (notes + next_token)
	var parsed getNotesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

type getPropertyDetailsResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// GetPropertyDetails fetches a property's full core + deal-structure payload.
// The backend "data" is a large, deeply-nested object ({property_detail,
// deal_structure}), so we pass it through as a generic map rather than
// mirroring ~25+ backend model types.
func (c *Client) GetSharedProperties(ctx context.Context, token string, limit int, ownership, status, lastKey string) (*SharedPropertiesPage, error) {
	// Only send params the caller actually provided; the backend defaults
	// limit to 10 on its own.
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if ownership != "" {
		q.Set("ownership", ownership)
	}
	if status != "" {
		q.Set("status", status)
	}
	if lastKey != "" {
		q.Set("lastKey", lastKey)
	}

	requrl := c.baseURL + "/connect/v1/property/list"
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	// GET has no body.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get shared properties failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// decode the double-nested envelope, then flatten into the clean page shape
	var parsed getSharedPropertiesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &SharedPropertiesPage{
		Properties: parsed.Data.Data,
		LastKey:    parsed.Data.LastKey,
	}, nil
}

// PropertySearchParams holds the wire-ready query parameters for a property
// search. Every field is optional; empty strings and false bools are omitted.
// Multi-value fields (HomeTypes, DealTypes, Tags) are comma-separated strings,
// already joined by the caller. Numeric fields stay as strings — the backend
// parses them and silently skips any that don't parse.
type PropertySearchParams struct {
	Address   string
	City      string
	ZipCode   string
	HomeTypes string
	YearBuilt string
	DealTypes string
	MinPrice  string
	MaxPrice  string
	Beds      string
	ExactBed  bool
	Baths     string
	ExactBath bool
	Tags      string
	Limit     string
}

// SearchProperties searches the caller's own and shared draft properties by
// location and filters. Despite being a POST, every parameter goes in the query
// string and the body is empty. The response is NOT the standard envelope: on
// success it's a bare JSON array of property objects, so we decode it directly.
// A failure comes back as HTTP 502 with {"error": "..."}.
func (c *Client) SearchProperties(ctx context.Context, token string, p PropertySearchParams) ([]map[string]interface{}, error) {
	q := url.Values{}
	set := func(key, val string) {
		if val != "" {
			q.Set(key, val)
		}
	}
	set("address", p.Address)
	set("city", p.City)
	set("zipCode", p.ZipCode)
	set("homeTypes", p.HomeTypes)
	set("yearBuilt", p.YearBuilt)
	set("dealTypes", p.DealTypes)
	set("minPrice", p.MinPrice)
	set("maxPrice", p.MaxPrice)
	set("beds", p.Beds)
	set("baths", p.Baths)
	set("tags", p.Tags)
	set("limit", p.Limit)
	// The exact-match flags default to "or more"; only send them when true.
	if p.ExactBed {
		q.Set("exactBed", "true")
	}
	if p.ExactBath {
		q.Set("exactBath", "true")
	}

	requrl := c.baseURL + "/connect/v1/search/property"
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	// POST with an empty body — the handler reads params from the query string.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search properties failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// Success is a bare JSON array (an empty result set is []), not an envelope.
	var results []map[string]interface{}
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// ConnectionSearchResult is the raw (non-enveloped) body of the user/connection
// search endpoint: unlike most endpoints it does NOT use the standard
// {success,data,message} envelope, so we decode it directly. Total is the count
// in THIS page (not a global total); NextCursor is empty when there are no more
// pages and only advances in LIST mode.
type ConnectionSearchResult struct {
	Connections []Connection `json:"connections"`
	Total       int          `json:"total"`
	Size        int          `json:"size"`
	NextCursor  string       `json:"next_cursor"`
	Message     string       `json:"message"`
}

// Connection is one entry in a connection search: the connection's profile plus
// the relationship state. Status and Direction are opaque strings passed through
// from the search service.
type Connection struct {
	User      ConnectionUser `json:"user"`
	Status    string         `json:"status"`
	Direction string         `json:"direction"`
}

// ConnectionUser is a connection's public profile. Its ID is what you pass as
// userID to ShareProperty.
type ConnectionUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Expertise string `json:"expertise"`
	City      string `json:"city"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	Img       string `json:"img"`
}

// SearchUsers searches the authenticated user's connections (their own network,
// not all users). The backend infers the search mode from which params are set:
// none => LIST (paginated via lastCursor), name => NAME, expertise and/or city
// => FILTER. size caps the page (default 15, max 50 server-side). The response
// is raw JSON (no envelope); a failure comes back as a non-200 with a
// {"error": "..."} body, which we surface as an error.
func (c *Client) SearchUsers(ctx context.Context, token, name, expertise, city string, size int, lastCursor string) (*ConnectionSearchResult, error) {
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}
	if expertise != "" {
		q.Set("expertise", expertise)
	}
	if city != "" {
		q.Set("city", city)
	}
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	if lastCursor != "" {
		q.Set("lastCursor", lastCursor)
	}

	requrl := c.baseURL + "/connect/v1/search/users"
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	// GET has no body.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search users failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// Raw body (not the standard envelope) — decode directly.
	var result ConnectionSearchResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// createFolderResponse decodes the standard envelope; the created folder object
// lives under `data`. Its exact shape is left dynamic (map) since we only pass
// it back through to the caller.
type createFolderResponse struct {
	Data map[string]interface{} `json:"data"`
}

// CreateFolder creates a folder for the caller. Like the search endpoint, the
// params go in the query string (name is required; parentID nests the new
// folder under an existing one) and the body is empty. Returns the created
// folder object on HTTP 201.
func (c *Client) CreateFolder(ctx context.Context, token, name, parentID, description string) (map[string]interface{}, error) {
	q := url.Values{}
	q.Set("name", name)
	if parentID != "" {
		q.Set("folder_id", parentID)
	}
	if description != "" {
		q.Set("description", description)
	}

	requrl := c.baseURL + "/connect/v1/folders?" + q.Encode()

	// POST with an empty body — the handler reads params from the query string.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create folder failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed createFolderResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return parsed.Data, nil
}

// NotificationsPage is the clean, tool-facing shape for a page of notifications.
type NotificationsPage struct {
	Items      []map[string]interface{} `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
}

// getUnreadNotificationsResponse decodes the standard envelope; the page lives
// under `data`.
type getUnreadNotificationsResponse struct {
	Data NotificationsPage `json:"data"`
}

// GetUnreadNotifications returns the caller's active (non-dismissed) unread
// notifications, paginated. limit defaults to 20 (max 100) server-side when
// omitted; cursor pages through results.
func (c *Client) GetUnreadNotifications(ctx context.Context, token string, limit int, cursor string) (*NotificationsPage, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}

	requrl := c.baseURL + "/connect/v1/notifications/unread"
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	// GET has no body.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get unread notifications failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed getUnreadNotificationsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

// GetComps reads the latest comp report for a property under a given comp type
// (exit strategy, e.g. "sold", "rental"). Both propertyID and compType are
// required by the backend. The call returns HTTP 200 regardless of state; the
// caller inspects CompsResult.Status to know what actually happened.
func (c *Client) GetComps(ctx context.Context, token, propertyID, compType string) (*CompsResult, error) {
	q := url.Values{}
	q.Set("propertyId", propertyID)
	q.Set("compType", compType)

	requrl := c.baseURL + "/connect/v1/comps?" + q.Encode()

	// GET has no body.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get comps failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed getCompsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

// RunComps starts a (billed, asynchronous) comp generation job for the subject
// property. Every outcome the backend models — accepted plus the billing/
// validation cases — comes back as a normal *RunCompsResult (read its Status).
// Only auth/transport failures or an unrecognizable body return a Go error.
func (c *Client) RunComps(ctx context.Context, token string, req RunCompsRequest) (*RunCompsResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	requrl := c.baseURL + "/connect/v1/run-comps"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var env runCompsEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("run comps failed: status %d, body %s", resp.StatusCode, respBody)
	}
	// Success (202) carries the result in `data`; modeled billing/validation
	// outcomes carry it in `error.details`. Either is a valid result to surface.
	if env.Data != nil && env.Data.Status != "" {
		return env.Data, nil
	}
	if env.Error != nil && env.Error.Details != nil && env.Error.Details.Status != "" {
		return env.Error.Details, nil
	}
	// PROPERTY_NOT_FOUND (404) is returned as an error envelope with no
	// `details`, so map it to a clean status using the human-readable message.
	if env.Error != nil && env.Error.Code == "PROPERTY_NOT_FOUND" {
		return &RunCompsResult{Status: "property_not_found", Message: env.Error.Message}, nil
	}
	// Nothing we recognize (e.g. 401 auth failure) — surface as an error.
	return nil, fmt.Errorf("run comps failed: status %d, body %s", resp.StatusCode, respBody)
}

func (c *Client) GetPropertyDetails(ctx context.Context, token, propertyID string) (map[string]interface{}, error) {
	requrl := c.baseURL + "/connect/v1/property/details/" + url.PathEscape(propertyID)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get property details failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed getPropertyDetailsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return parsed.Data, nil
}

func (c *Client) CreateProperty(ctx context.Context, token string, req CreatePropertyRequest) (*Property, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/connect/v1/property", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	//sending the api request

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create property failed: status %d, body %s", resp.StatusCode, respBody)
	}

	// decode the envelopes and return the inner property
	var parsed createPropertyResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil

}

// --- CRM (push a lead to the connected GoHighLevel CRM) ---

// CRMContact is the nested contact block of a CRM push. ContactType is required
// (property_owner|agent|jv_partner|bird_dog|buyer). UseStoredData is a *bool so
// we can tell "unset" (nil → omitted → backend default true) apart from an
// explicit false; a plain bool would zero-value to false and silently flip that
// default.
type CRMContact struct {
	ContactType   string   `json:"contactType"`
	UseStoredData *bool    `json:"useStoredData,omitempty"`
	FirstName     string   `json:"firstName,omitempty"`
	LastName      string   `json:"lastName,omitempty"`
	Email         string   `json:"email,omitempty"`
	Phone         string   `json:"phone,omitempty"`
	AssignedTo    string   `json:"assignedTo,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

// CRMOpportunity is the optional opportunity block. When present, all three
// fields are required by the backend.
type CRMOpportunity struct {
	PipelineID string `json:"pipelineId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

// CRMPushRequest is the POST /crm/contacts body.
type CRMPushRequest struct {
	PropertyID  string          `json:"propertyId"`
	LocationID  string          `json:"locationId,omitempty"`
	Contact     CRMContact      `json:"contact"`
	Opportunity *CRMOpportunity `json:"opportunity,omitempty"`
}

// CRMPushOpportunity is the opportunity outcome nested in a push result. An
// opportunity failure does NOT fail the push, so Created may be false with a
// human-readable Error even on a 201.
type CRMPushOpportunity struct {
	Created       bool   `json:"created"`
	OpportunityID string `json:"opportunityId,omitempty"`
	Error         string `json:"error,omitempty"`
}

// CRMPushResult is the data payload of a successful push. ContactSource is
// "property" (identity came from the property's stored data) or "request" (from
// the contact block).
type CRMPushResult struct {
	ContactID     string              `json:"contactId"`
	LocationID    string              `json:"locationId"`
	PropertyID    string              `json:"propertyId"`
	ContactSource string              `json:"contactSource"`
	Opportunity   *CRMPushOpportunity `json:"opportunity,omitempty"`
}

type crmPushResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Data    CRMPushResult `json:"data"`
}

// PushToCRM pushes a property to the caller's connected CRM as a new contact
// (optionally creating an opportunity and attaching notes). Success is HTTP 201
// with the push result. Modeled failures (already in CRM, location required,
// reconnect required, CRM rejected, etc.) come back as non-201s with the
// standard error envelope, which we surface as a Go error carrying the body.
func (c *Client) PushToCRM(ctx context.Context, token string, req CRMPushRequest) (*CRMPushResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	requrl := c.baseURL + "/connect/v1/crm/contacts"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("push to crm failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed crmPushResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

// crmGet is a shared helper for the read-only CRM picker endpoints: a GET with
// an optional locationId query param that decodes the standard envelope's `data`
// into out. Non-200 is surfaced as a Go error carrying the body.
func (c *Client) crmGet(ctx context.Context, token, path, locationID string, out interface{}) error {
	q := url.Values{}
	if locationID != "" {
		q.Set("locationId", locationID)
	}
	requrl := c.baseURL + path
	if encoded := q.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requrl, nil)
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("crm get %s failed: status %d, body %s", path, resp.StatusCode, respBody)
	}

	// Standard envelope: pull `data` out and decode it into the caller's struct.
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, out)
}

// CRMConnectionsResult lists the caller's connected CRM accounts. Each entry has
// locationId, accountName, and connectedAt (passed through as a map since the
// exact shape isn't pinned down here).
type CRMConnectionsResult struct {
	Connections []map[string]interface{} `json:"connections"`
	Total       int                      `json:"total"`
}

func (c *Client) GetCRMConnections(ctx context.Context, token string) (*CRMConnectionsResult, error) {
	var out CRMConnectionsResult
	if err := c.crmGet(ctx, token, "/connect/v1/crm/connections", "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CRMUsersResult lists assignable CRM users for a location. Each entry has id,
// name, email.
type CRMUsersResult struct {
	LocationID string                   `json:"locationId"`
	Users      []map[string]interface{} `json:"users"`
	Total      int                      `json:"total"`
}

func (c *Client) GetCRMUsers(ctx context.Context, token, locationID string) (*CRMUsersResult, error) {
	var out CRMUsersResult
	if err := c.crmGet(ctx, token, "/connect/v1/crm/users", locationID, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CRMTagsResult lists the tag names available in a CRM location.
type CRMTagsResult struct {
	LocationID string   `json:"locationId"`
	Tags       []string `json:"tags"`
	Total      int      `json:"total"`
}

func (c *Client) GetCRMTags(ctx context.Context, token, locationID string) (*CRMTagsResult, error) {
	var out CRMTagsResult
	if err := c.crmGet(ctx, token, "/connect/v1/crm/tags", locationID, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CRMPipelinesResult lists the pipelines (and their stages) in a CRM location.
// Each pipeline has id, name, and stages [{id, name, position}].
type CRMPipelinesResult struct {
	LocationID string                   `json:"locationId"`
	Pipelines  []map[string]interface{} `json:"pipelines"`
	Total      int                      `json:"total"`
}

func (c *Client) GetCRMPipelines(ctx context.Context, token, locationID string) (*CRMPipelinesResult, error) {
	var out CRMPipelinesResult
	if err := c.crmGet(ctx, token, "/connect/v1/crm/pipelines", locationID, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CRMCreateOpportunityRequest is the POST /crm/opportunities body. Everything
// but LocationID is required.
type CRMCreateOpportunityRequest struct {
	LocationID string `json:"locationId,omitempty"`
	PipelineID string `json:"pipelineId"`
	ContactID  string `json:"contactId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

// CRMOpportunityResult is the data payload from creating an opportunity for an
// existing contact.
type CRMOpportunityResult struct {
	OpportunityID string `json:"opportunityId"`
	LocationID    string `json:"locationId"`
	PipelineID    string `json:"pipelineId"`
	ContactID     string `json:"contactId"`
	Name          string `json:"name"`
	Status        string `json:"status"`
}

type crmOpportunityResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Data    CRMOpportunityResult `json:"data"`
}

// CreateCRMOpportunity creates an opportunity for a contact that already exists
// in the CRM (typically to retry after a non-fatal opportunity failure during a
// push). Success is HTTP 201; modeled failures come back as non-201s with the
// standard error envelope, surfaced as a Go error carrying the body.
func (c *Client) CreateCRMOpportunity(ctx context.Context, token string, req CRMCreateOpportunityRequest) (*CRMOpportunityResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	requrl := c.baseURL + "/connect/v1/crm/opportunities"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create crm opportunity failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed crmOpportunityResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}

// crmAddNoteRequest is the POST body for adding a note to a pushed contact.
type crmAddNoteRequest struct {
	Note       string `json:"note"`
	LocationID string `json:"locationId,omitempty"`
}

// CRMAddNoteResult is the data payload from adding a note to the contact a
// previous push created.
type CRMAddNoteResult struct {
	PropertyID string `json:"propertyId"`
	ContactID  string `json:"contactId"`
	LocationID string `json:"locationId"`
}

type crmAddNoteResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    CRMAddNoteResult `json:"data"`
}

// AddCRMNote adds a note to the CRM contact created by a previous push of the
// property. Success is HTTP 201. A property that was never pushed comes back as
// 404 PROPERTY_NOT_IN_CRM (a non-201), surfaced as a Go error carrying the body.
func (c *Client) AddCRMNote(ctx context.Context, token, propertyID, note, locationID string) (*CRMAddNoteResult, error) {
	body, err := json.Marshal(crmAddNoteRequest{Note: note, LocationID: locationID})
	if err != nil {
		return nil, err
	}

	requrl := c.baseURL + "/connect/v1/crm/properties/" + url.PathEscape(propertyID) + "/notes"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requrl, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("add crm note failed: status %d, body %s", resp.StatusCode, respBody)
	}

	var parsed crmAddNoteResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Data, nil
}
