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
