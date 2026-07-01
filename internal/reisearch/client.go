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

// CompSubject is the property we want comps generated for. PropertyID and
// Address are the only fields the backend requires; the rest refine matching.
// Latitude/Longitude are float64 here (the comps API wants numbers), even though
// the stored property record keeps lat/long as strings.
type CompSubject struct {
	PropertyID string  `json:"propertyId"`
	Address    string  `json:"address"`
	City       string  `json:"city,omitempty"`
	State      string  `json:"state,omitempty"`
	ZipCode    string  `json:"zipCode,omitempty"`
	Latitude   float64 `json:"latitude,omitempty"`
	Longitude  float64 `json:"longitude,omitempty"`
	Bedrooms   string  `json:"bedrooms,omitempty"`
	Bathrooms  string  `json:"bathrooms,omitempty"`
}

// RunCompsRequest is the POST /run-comps body. One run covers all exit
// strategies — there is no compType.
type RunCompsRequest struct {
	Subject CompSubject `json:"subject"`
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
// success (202) and `error.details` for the modeled billing outcomes.
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
