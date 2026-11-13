package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// County parcel records (the Taxes tab): /connect/v1/parcels lookup (POST),
// create (PUT) and merge-patch (PATCH with If-Match). These write the shared
// county-parcel table, and only plans enabled for it may call them — every
// other plan gets 429 on every call. Uses apiDoWithHeaders so failures are
// *APIError.

// ParcelImmutableFields identify the record and can't be patched.
var ParcelImmutableFields = []string{"folio_number", "property_address", "ingested_at"}

// ParcelReadOnlyFields are server-managed and can't be sent on create or patch.
var ParcelReadOnlyFields = []string{"PK", "SK", "property_address_hash", "mailing_address_hash", "revision", "api_created_at", "api_updated_at", "api_updated_by"}

// ParcelFieldChanges lists what a PATCH changed.
type ParcelFieldChanges struct {
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

// ParcelResult is the lookup/create/patch response data. Revision is the
// value to pass back as expected_revision (sent as If-Match).
type ParcelResult struct {
	PK        string              `json:"pk"`
	SK        string              `json:"sk"`
	Folio     string              `json:"folio"`
	Revision  int64               `json:"revision"`
	MatchedBy string              `json:"matched_by,omitempty"`
	Fields    *ParcelFieldChanges `json:"fields,omitempty"`
	Record    map[string]any      `json:"record"`
}

func parcelResult(raw json.RawMessage) (*ParcelResult, error) {
	var out ParcelResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.Record == nil {
		out.Record = map[string]any{}
	}
	return &out, nil
}

// LookupParcel finds an existing record by free-text address, narrowed by
// folio when given. Read-only.
func (c *Client) LookupParcel(ctx context.Context, token, address, folio string) (*ParcelResult, error) {
	body := map[string]string{"address": address}
	if folio != "" {
		body["folio"] = folio
	}
	raw, err := c.apiDo(ctx, token, http.MethodPost, "/connect/v1/parcels/lookup", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	return parcelResult(raw)
}

// CreateParcel creates a new record (never overwrites; 409 PARCEL_EXISTS if
// it's already stored).
func (c *Client) CreateParcel(ctx context.Context, token string, parcel map[string]any) (*ParcelResult, error) {
	raw, err := c.apiDo(ctx, token, http.MethodPut, "/connect/v1/parcels", url.Values{}, parcel)
	if err != nil {
		return nil, err
	}
	return parcelResult(raw)
}

// PatchParcel applies an RFC 7396 merge patch to exactly (pk, PARCEL#folio),
// only if the stored revision still equals expectedRevision (If-Match).
func (c *Client) PatchParcel(ctx context.Context, token, pk, folio string, expectedRevision int64, patch map[string]any) (*ParcelResult, error) {
	q := url.Values{}
	q.Set("folio", folio)
	headers := map[string]string{"If-Match": `"` + strconv.FormatInt(expectedRevision, 10) + `"`}
	raw, err := c.apiDoWithHeaders(ctx, token, http.MethodPatch, "/connect/v1/parcels/"+url.PathEscape(pk), q, patch, headers)
	if err != nil {
		return nil, err
	}
	return parcelResult(raw)
}
