package reisearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PropertyImage is a single image from GET /property/{id}/image: the stable
// s3_key plus a short-lived presigned view URL. URL expires quickly — never
// cache it; re-list for a fresh one. Timestamps are epoch milliseconds.
type PropertyImage struct {
	S3Key            string                 `json:"s3_key"`
	URL              string                 `json:"url,omitempty"`
	CreatedAt        int64                  `json:"createdAt,omitempty"`
	Source           string                 `json:"source,omitempty"`
	Type             string                 `json:"type,omitempty"`
	TagType          string                 `json:"tagType,omitempty"`
	RenovationStatus string                 `json:"renovationStatus,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// PropertyImagesResult wraps the images list (a bare-slice tool output panics
// at AddTool time, so every result is an object).
type PropertyImagesResult struct {
	Images []PropertyImage `json:"images"`
	Count  int             `json:"count"`
}

// LedgerImageRef is one entry on the caller's per-(property, user) renovation
// ledger. EstimateID and SubmittedAt appear only on after/accepted refs.
type LedgerImageRef struct {
	S3Key            string `json:"s3_key"`
	CreatedAt        int64  `json:"createdAt,omitempty"`
	Source           string `json:"source,omitempty"`
	Type             string `json:"type,omitempty"`
	TagType          string `json:"tagType,omitempty"`
	RenovationStatus string `json:"renovationStatus,omitempty"`
	EstimateID       string `json:"estimateId,omitempty"`
	SubmittedAt      int64  `json:"submittedAt,omitempty"`
}

// ImageTagEntry is one image-tagging instruction for TagPropertyImages:
// which image (s3_key) is a "before" shot of which room (tag_type, canonical
// `root:instance` like "kitchen:1").
type ImageTagEntry struct {
	S3Key    string                 `json:"s3_key"`
	TagType  string                 `json:"tag_type"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TagImagesResult is PATCH /image/tags' response: the resulting "before"
// ledger entries for the caller.
type TagImagesResult struct {
	Before []LedgerImageRef `json:"before"`
}

// RenovationLineItem is one row of a renovation cost estimate (mirrors the
// backend's RenovationLineItemDTO; costs are dollars).
type RenovationLineItem struct {
	Name         string  `json:"name"`
	MaterialCost float64 `json:"materialCost,omitempty"`
	LaborCost    float64 `json:"laborCost,omitempty"`
	Total        float64 `json:"total,omitempty"`
}

// RenovationSummary totals a renovation estimate.
type RenovationSummary struct {
	TotalMaterials float64 `json:"totalMaterials,omitempty"`
	TotalLabor     float64 `json:"totalLabor,omitempty"`
	TotalCost      float64 `json:"totalCost,omitempty"`
}

// RenovationEstimate is the caller-supplied cost breakdown submitted with a
// render; the backend maps it onto an underwriting repairRenovation room.
// ThreadID/EstimateID are optional provenance carried into the room's ai block.
type RenovationEstimate struct {
	LineItems   []RenovationLineItem `json:"lineItems,omitempty"`
	Summary     *RenovationSummary   `json:"summary,omitempty"`
	FinishGrade string               `json:"finishGrade,omitempty"`
	RehabLevel  string               `json:"rehabLevel,omitempty"`
	ThreadID    string               `json:"threadId,omitempty"`
	EstimateID  string               `json:"estimateId,omitempty"`
}

type submitRenovationRequest struct {
	S3Key    string             `json:"s3_key"`
	TagType  string             `json:"tag_type"`
	Estimate RenovationEstimate `json:"estimate"`
}

// RenovationLedger is the caller's per-property renovation state. Sections not
// requested come back nil; the backend never returns the `original` bucket.
// Accepted is keyed by tag_type (the live render per room).
type RenovationLedger struct {
	PropertyID string                    `json:"property_id"`
	UserID     string                    `json:"user_id"`
	Accepted   map[string]LedgerImageRef `json:"accepted,omitempty"`
	Before     []LedgerImageRef          `json:"before,omitempty"`
	After      []LedgerImageRef          `json:"after,omitempty"`
}

// renoDo performs an authenticated JSON request against a property-image
// endpoint and decodes the standard envelope's `data` into out. All four
// renovation routes answer 200 on success; non-200 is surfaced as a Go error
// carrying the response body.
func (c *Client) renoDo(ctx context.Context, token, method, path string, query url.Values, body interface{}, out interface{}) error {
	requrl := c.baseURL + path
	if query != nil {
		if encoded := query.Encode(); encoded != "" {
			requrl += "?" + encoded
		}
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, method, requrl, reader)
	if err != nil {
		return err
	}
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
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
		return fmt.Errorf("property image request %s %s failed: status %d, body %s", method, path, resp.StatusCode, respBody)
	}

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return err
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// ListPropertyImages lists a property's images with short-lived view URLs and
// renovation tags/status. 404 (property not found) surfaces as an error.
func (c *Client) ListPropertyImages(ctx context.Context, token, propertyID string) (*PropertyImagesResult, error) {
	var out PropertyImagesResult
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image"
	if err := c.renoDo(ctx, token, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	out.Count = len(out.Images)
	return &out, nil
}

// TagPropertyImages records renovation "before" tags for the given images on
// the caller's per-user ledger; the shared property record is not modified.
func (c *Client) TagPropertyImages(ctx context.Context, token, propertyID string, images []ImageTagEntry) (*TagImagesResult, error) {
	var out TagImagesResult
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image/tags"
	body := struct {
		Images []ImageTagEntry `json:"images"`
	}{Images: images}
	if err := c.renoDo(ctx, token, http.MethodPatch, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type saveRenovationImageURLRequest struct {
	ImageURL string `json:"image_url"`
	TagType  string `json:"tag_type"`
}

// SaveRenovationImageResult is the save endpoint's response: the s3_key of the
// stored render. The image is parked in S3 but NOT tracked until that s3_key is
// passed to SubmitPropertyRenovation.
type SaveRenovationImageResult struct {
	S3Key string `json:"s3_key"`
}

// SaveRenovationImageFromURL saves a renovation render by pointing the backend at
// a public http/https URL (the JSON-body form of POST .../image/renovation),
// which it fetches server-side. The multipart byte-upload form of the same route
// can't be driven from an MCP — a tool call is JSON, never a file body — so this
// URL form is the only save path we expose. Returns the s3_key to hand to
// SubmitPropertyRenovation. Fetch failures (unreachable, refused, timeout,
// upstream 404) surface as one 400 the backend makes deliberately
// indistinguishable.
func (c *Client) SaveRenovationImageFromURL(ctx context.Context, token, propertyID, imageURL, tagType string) (*SaveRenovationImageResult, error) {
	var out SaveRenovationImageResult
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image/renovation"
	body := saveRenovationImageURLRequest{ImageURL: imageURL, TagType: tagType}
	if err := c.renoDo(ctx, token, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitPropertyRenovation submits an already-uploaded render (by s3_key) plus
// its cost estimate into the property's underwriting for the caller. Returns
// the repairRenovation room data the backend wrote (dynamic map passthrough).
// A 500 "failed to submit renovation to underwriting" may not have landed —
// retrying the same s3_key + tag_type is safe (the upsert is idempotent).
func (c *Client) SubmitPropertyRenovation(ctx context.Context, token, propertyID, s3Key, tagType string, estimate RenovationEstimate) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image/renovation/submit"
	body := submitRenovationRequest{S3Key: s3Key, TagType: tagType, Estimate: estimate}
	if err := c.renoDo(ctx, token, http.MethodPost, path, nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRenovationLedger reads the caller's renovation ledger for a property.
// sections may hold any of "accepted", "before", "after"; empty returns all
// three. The backend accepts comma-joined values (no spaces — repo convention).
func (c *Client) GetRenovationLedger(ctx context.Context, token, propertyID string, sections []string) (*RenovationLedger, error) {
	var out RenovationLedger
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image/ledger"
	var q url.Values
	if len(sections) > 0 {
		q = url.Values{}
		q.Set("section", strings.Join(sections, ","))
	}
	if err := c.renoDo(ctx, token, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
