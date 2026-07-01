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
