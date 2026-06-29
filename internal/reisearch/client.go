package reisearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func NewClient(baseUrl string) *Client {
	return &Client{
		baseURL:    baseUrl,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
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
