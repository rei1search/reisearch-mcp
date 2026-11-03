package reisearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Underwriting v6 (api-pub, verified 2026-11-03). Every call carries a
// strategy (coLiving | fixFlip) and all section data lives inside per-user
// named versions. Writes select a version with versionId or createNew=true
// and return the resolved versionId.

// UnderwritingStrategies are the strategies the backend implements.
var UnderwritingStrategies = []string{"coLiving", "fixFlip"}

// UnderwritingSectionPaths mirrors the backend's Coliving/FixFlipPathRegistry:
// strategy → section → allowed top-level PATCH paths. A section missing from a
// strategy's map is not supported by that strategy.
var UnderwritingSectionPaths = map[string]map[string][]string{
	"coLiving": {
		"postRenovation":        {"bedrooms", "bathrooms", "sqft", "aduPrivateSuite"},
		"repairRenovation":      repairRenovationPaths,
		"appliances":            appliancePaths,
		"coliving":              {"securityCameras", "smartLocks", "smartThermostat", "doorSensors", "itCloset", "propertyPictures", "complianceItems", "calculatedTotal", "estimatedTotal"},
		"furnishing":            {"bedroomsFurnishing", "bathroomsFurnishing", "kitchenFurnishing", "commonAreasFurnishing", "cleaningSupplies", "calculatedTotal", "estimatedTotal"},
		"operatingRevenues":     {"totalBeds", "totalBaths", "rentalRevenue", "rentalLateFee", "sharedLaundryFee", "miscellaneousIncome", "movingFee", "calculatedTotal", "estimatedTotal"},
		"operatingExpenses":     {"bookingServiceFee", "utilities", "cleaningService", "maintenance", "management", "totalOperatingExpenses"},
		"privateMoneyFinancing": {"privateMoneyLender", "privateMoneyPartner", "hardMoneyLender", "calculatedTotal", "estimatedTotal"},
		"holdingCost":           {"total", "holdingPeriod", "unitAmount"},
		"fundingSource":         {"debtFinancing", "cashEquity", "creativeFinance"},
	},
	"fixFlip": {
		"postRenovation":   {"bedrooms", "bathrooms", "sqft", "aduPrivateSuite"},
		"repairRenovation": repairRenovationPaths,
		"appliances":       appliancePaths,
		"holdingCost":      {"total", "holdingPeriod", "unitAmount"},
		"fundingSource":    {"debtFinancing", "cashEquity", "creativeFinance"},
		"saleExit":         {"salePrice", "agentCommission", "sellerClosingCost"},
	},
}

var repairRenovationPaths = []string{"interiorRenovations", "aduAdditionDetails", "hvacDetails", "electricalCost", "plumbingCost", "exteriorRenovations", "repairContingency", "estimatedTotal", "calculatedTotal"}
var appliancePaths = []string{"refrigerator", "miniFridge", "washerDryer", "waterHeater", "stove", "microwave", "airFryer", "toaster", "calculatedTotal", "estimatedTotal"}

// UnderwritingRoomAreas are the repairRenovation interior areas that hold
// per-room entries (backend AllowedInteriorFields).
var UnderwritingRoomAreas = []string{"bedrooms", "bathrooms", "kitchen", "livingRoom", "diningRoom", "laundry", "fireplace", "attic", "basement", "generalUpdates", "miscellaneous"}

// UnderwritingSectionsFor lists a strategy's sections, sorted (nil for an
// unknown strategy).
func UnderwritingSectionsFor(strategy string) []string {
	sections, ok := UnderwritingSectionPaths[strategy]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(sections))
	for s := range sections {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// UnderwritingAPIError is a non-2xx underwriting response, decoded from the
// standard envelope's error block so tools can branch on Status/Code.
type UnderwritingAPIError struct {
	Status  int
	Code    string
	Message string
	Details json.RawMessage
}

func (e *UnderwritingAPIError) Error() string {
	msg := fmt.Sprintf("underwriting API error %d %s: %s", e.Status, e.Code, e.Message)
	if len(e.Details) > 0 && string(e.Details) != "null" {
		msg += " (details: " + string(e.Details) + ")"
	}
	return msg
}

// VersionSelector picks the version a write lands in: VersionID for an
// existing one, or CreateNew (+ optional VersionName) to start a new one.
type VersionSelector struct {
	VersionID   string
	CreateNew   bool
	VersionName string
}

func (v VersionSelector) apply(q url.Values) {
	if v.VersionID != "" {
		q.Set("versionId", v.VersionID)
	}
	if v.CreateNew {
		q.Set("createNew", "true")
	}
	if v.VersionName != "" {
		q.Set("versionName", v.VersionName)
	}
}

// uwDo performs an authenticated underwriting request. Unlike renoDo it
// accepts any 2xx (section create answers 201), and on failure returns an
// *UnderwritingAPIError. It returns the envelope's raw `data` ("null" or empty
// when the backend sent none).
func (c *Client) uwDo(ctx context.Context, token, method, path string, query url.Values, body interface{}) (json.RawMessage, error) {
	requrl := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		requrl += "?" + encoded
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, method, requrl, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
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

	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(respBody, &env)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &UnderwritingAPIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(respBody))}
		if decodeErr == nil && env.Error != nil {
			apiErr.Code = env.Error.Code
			apiErr.Message = env.Error.Message
			apiErr.Details = env.Error.Details
		}
		return nil, apiErr
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("underwriting %s %s: unreadable response: %w", method, path, decodeErr)
	}
	return env.Data, nil
}

func uwBase(propertyID string) string {
	return "/connect/v1/underwriting/" + url.PathEscape(propertyID)
}

func strategyQuery(strategy string) url.Values {
	q := url.Values{}
	q.Set("strategy", strategy)
	return q
}

func isEmptyData(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "[]" || s == "{}"
}

// ---------------------------------------------------------------- versions

// UnderwritingVersion is one version's metadata. Timestamps are epoch
// MILLISECONDS; the *ISO fields are added client-side for readability.
type UnderwritingVersion struct {
	VersionID       string   `json:"versionId"`
	Version         int      `json:"version,omitempty"`
	VersionName     string   `json:"versionName"`
	Strategy        string   `json:"strategy,omitempty"`
	SourceVersionID *string  `json:"sourceVersionId,omitempty"`
	CreatedByID     string   `json:"createdById,omitempty"`
	IsActive        bool     `json:"isActive"`
	DealScore       *float64 `json:"dealScore,omitempty"`
	CreatedAt       int64    `json:"createdAt,omitempty"`
	UpdatedAt       int64    `json:"updatedAt,omitempty"`
	CreatedAtISO    string   `json:"createdAtISO,omitempty"`
	UpdatedAtISO    string   `json:"updatedAtISO,omitempty"`
}

// UnderwritingVersionList is GET /versions. Versions is never nil (the API
// sends null when there are none).
type UnderwritingVersionList struct {
	Scope    string                `json:"scope"`
	Count    int                   `json:"count"`
	Versions []UnderwritingVersion `json:"versions"`
}

func msToISO(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// ListUnderwritingVersions lists versions on a property. scope is "mine" or
// "all" (the API defaults to "all" when empty).
func (c *Client) ListUnderwritingVersions(ctx context.Context, token, propertyID, strategy, scope string) (*UnderwritingVersionList, error) {
	q := strategyQuery(strategy)
	if scope != "" {
		q.Set("scope", scope)
	}
	raw, err := c.uwDo(ctx, token, http.MethodGet, uwBase(propertyID)+"/versions", q, nil)
	if err != nil {
		return nil, err
	}
	var out UnderwritingVersionList
	if !isEmptyData(raw) {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	if out.Versions == nil {
		out.Versions = []UnderwritingVersion{}
	}
	for i := range out.Versions {
		out.Versions[i].CreatedAtISO = msToISO(out.Versions[i].CreatedAt)
		out.Versions[i].UpdatedAtISO = msToISO(out.Versions[i].UpdatedAt)
	}
	out.Count = len(out.Versions)
	return &out, nil
}

// CloneVersionResult is the clone response.
type CloneVersionResult struct {
	SourceVersionID string `json:"sourceVersionId"`
	VersionID       string `json:"versionId"`
	VersionName     string `json:"versionName"`
	Cloned          bool   `json:"cloned"`
}

// CloneUnderwritingVersion copies a whole version into a new one. The route
// needs a {section} segment that clone ignores, so postRenovation is
// hard-coded. createNew must never be sent with clone. Not idempotent.
func (c *Client) CloneUnderwritingVersion(ctx context.Context, token, propertyID, strategy, sourceVersionID, versionName string) (*CloneVersionResult, error) {
	q := strategyQuery(strategy)
	q.Set("clone", "true")
	q.Set("versionId", sourceVersionID)
	if versionName != "" {
		q.Set("versionName", versionName)
	}
	raw, err := c.uwDo(ctx, token, http.MethodPost, uwBase(propertyID)+"/postRenovation", q, nil)
	if err != nil {
		return nil, err
	}
	var out CloneVersionResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VersionChangeResult is the response of rename / deal score / delete.
type VersionChangeResult struct {
	PropertyID  string   `json:"propertyId"`
	VersionID   string   `json:"versionId"`
	VersionName string   `json:"versionName,omitempty"`
	DealScore   *float64 `json:"dealScore,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// RenameUnderwritingVersion renames one of the caller's versions. The backend
// answers 500 "name unchanged" for a no-op rename, so callers should compare
// first (the tool does).
func (c *Client) RenameUnderwritingVersion(ctx context.Context, token, propertyID, strategy, versionID, versionName string) (*VersionChangeResult, error) {
	q := strategyQuery(strategy)
	q.Set("versionId", versionID)
	q.Set("versionName", versionName)
	raw, err := c.uwDo(ctx, token, http.MethodPatch, uwBase(propertyID)+"/versions/rename", q, nil)
	if err != nil {
		return nil, err
	}
	var out VersionChangeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetUnderwritingDealScore sets a version's deal score (0–100).
func (c *Client) SetUnderwritingDealScore(ctx context.Context, token, propertyID, strategy, versionID string, score float64) (*VersionChangeResult, error) {
	path := uwBase(propertyID) + "/versions/" + url.PathEscape(versionID) + "/dealScore"
	body := map[string]float64{"dealScore": score}
	raw, err := c.uwDo(ctx, token, http.MethodPatch, path, strategyQuery(strategy), body)
	if err != nil {
		return nil, err
	}
	var out VersionChangeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteUnderwritingVersion removes a whole version and every section in it.
func (c *Client) DeleteUnderwritingVersion(ctx context.Context, token, propertyID, strategy, versionID string) (*VersionChangeResult, error) {
	path := uwBase(propertyID) + "/versions/" + url.PathEscape(versionID)
	raw, err := c.uwDo(ctx, token, http.MethodDelete, path, strategyQuery(strategy), nil)
	if err != nil {
		return nil, err
	}
	var out VersionChangeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- sections

// GetUnderwritingSection reads one section. versionID is optional (empty =
// the caller's newest version). Returns nil, nil when the section has no data
// yet — that is not an error.
func (c *Client) GetUnderwritingSection(ctx context.Context, token, propertyID, strategy, section, versionID string) (map[string]interface{}, error) {
	q := strategyQuery(strategy)
	if versionID != "" {
		q.Set("versionId", versionID)
	}
	raw, err := c.uwDo(ctx, token, http.MethodGet, uwBase(propertyID)+"/"+url.PathEscape(section), q, nil)
	if err != nil {
		return nil, err
	}
	if isEmptyData(raw) {
		return nil, nil
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// uwBookkeepingKeys are storage fields GET /all leaves on every section map.
var uwBookkeepingKeys = []string{"propertyId", "createdById", "createdAt", "updatedAt"}

// GetAllUnderwriting returns one entry per (creator, version) on the property
// for the strategy — INCLUDING collaborators' versions. Bookkeeping keys are
// stripped from each section.
func (c *Client) GetAllUnderwriting(ctx context.Context, token, propertyID, strategy string) ([]map[string]interface{}, error) {
	raw, err := c.uwDo(ctx, token, http.MethodGet, uwBase(propertyID)+"/all", strategyQuery(strategy), nil)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	if isEmptyData(raw) {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for _, entry := range out {
		sections, ok := entry["underwriting"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, sec := range sections {
			if m, ok := sec.(map[string]interface{}); ok {
				for _, k := range uwBookkeepingKeys {
					delete(m, k)
				}
			}
		}
	}
	return out, nil
}

// CreateSectionResult is POST /{section}'s response.
type CreateSectionResult struct {
	VersionID   string                 `json:"versionId"`
	VersionName string                 `json:"versionName,omitempty"`
	Section     string                 `json:"section"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Cloned      bool                   `json:"cloned"`
}

// CreateUnderwritingSection creates a section in a version. 409 CONFLICT when
// the section already exists in that version (use UpdateUnderwritingSection).
func (c *Client) CreateUnderwritingSection(ctx context.Context, token, propertyID, strategy, section string, sel VersionSelector, data map[string]interface{}) (*CreateSectionResult, error) {
	q := strategyQuery(strategy)
	sel.apply(q)
	raw, err := c.uwDo(ctx, token, http.MethodPost, uwBase(propertyID)+"/"+url.PathEscape(section), q, data)
	if err != nil {
		return nil, err
	}
	var out CreateSectionResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSectionResult is PATCH /{section}/{path}'s response. Data echoes the
// submitted value, NOT the merged section.
type UpdateSectionResult struct {
	VersionID string      `json:"versionId"`
	Section   string      `json:"section"`
	Path      string      `json:"path"`
	Data      interface{} `json:"data"`
}

// UpdateUnderwritingSection writes one top-level path of a section (creating
// the section if missing). Objects deep-merge; arrays and scalars replace.
func (c *Client) UpdateUnderwritingSection(ctx context.Context, token, propertyID, strategy, section, path string, sel VersionSelector, value interface{}) (*UpdateSectionResult, error) {
	q := strategyQuery(strategy)
	sel.VersionName = "" // ignored by this route
	sel.apply(q)
	reqPath := uwBase(propertyID) + "/" + url.PathEscape(section) + "/" + url.PathEscape(path)
	raw, err := c.uwDo(ctx, token, http.MethodPatch, reqPath, q, value)
	if err != nil {
		return nil, err
	}
	var out UpdateSectionResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.Path = path
	return &out, nil
}

// ---------------------------------------------------------------- rooms

// UnderwritingCost mirrors the backend's common.CostBreakdown (dollars).
type UnderwritingCost struct {
	MaterialCost *float64 `json:"materialCost,omitempty"`
	LaborCost    *float64 `json:"laborCost,omitempty"`
	SquareFeet   *float64 `json:"sqft,omitempty"`
	Total        *float64 `json:"total,omitempty"`
}

// UnderwritingLineItem is one row of a room: a name plus a nested cost
// breakdown (NOT flat materialCost/laborCost like the renovation estimate).
type UnderwritingLineItem struct {
	Name string            `json:"name"`
	Cost *UnderwritingCost `json:"cost,omitempty"`
}

// UnderwritingRoomAI is a room's AI provenance. ThreadID and EstimateID are
// required by the backend whenever ai is sent.
type UnderwritingRoomAI struct {
	ThreadID            string  `json:"threadId"`
	EstimateID          string  `json:"estimateId"`
	GeneratedImageS3Key *string `json:"generatedImageS3Key,omitempty"`
	FinishGrade         *string `json:"finishGrade,omitempty"`
	RehabLevel          *string `json:"rehabLevel,omitempty"`
}

// UnderwritingRoom mirrors the backend's SectionEntry (minus tag, which comes
// from the URL). The backend rejects unknown fields, so this is typed.
type UnderwritingRoom struct {
	Name          *string                `json:"name,omitempty"`
	Source        *string                `json:"source,omitempty"`
	SquareFootage *float64               `json:"squareFootage,omitempty"`
	HasEnSuite    *bool                  `json:"hasEnSuite,omitempty"`
	Cost          *UnderwritingCost      `json:"cost,omitempty"`
	LineItems     []UnderwritingLineItem `json:"lineItems,omitempty"`
	AI            *UnderwritingRoomAI    `json:"ai,omitempty"`
}

// RoomResult is the room upsert response: the resolved version plus an echo
// of the patch (not the merged room).
type RoomResult struct {
	VersionID string                 `json:"versionId"`
	Room      map[string]interface{} `json:"room"`
}

// roomPath builds the per-room route. PathEscape leaves ':' alone, so it is
// escaped explicitly (kitchen:1 → kitchen%3A1), as verified against prod.
func roomPath(propertyID, area, tag string) string {
	escapedTag := strings.ReplaceAll(url.PathEscape(tag), ":", "%3A")
	return uwBase(propertyID) + "/repairRenovation/" + url.PathEscape(area) + "/rooms/" + escapedTag
}

// UpsertUnderwritingRoom patches one room entry under
// repairRenovation.interiorRenovations.{area}.items.{tag}. Top-level room
// keys merge with what's stored.
func (c *Client) UpsertUnderwritingRoom(ctx context.Context, token, propertyID, strategy, area, tag string, sel VersionSelector, room UnderwritingRoom) (*RoomResult, error) {
	q := strategyQuery(strategy)
	sel.apply(q)
	raw, err := c.uwDo(ctx, token, http.MethodPatch, roomPath(propertyID, area, tag), q, room)
	if err != nil {
		return nil, err
	}
	var out RoomResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRoomResult is the room delete response.
type DeleteRoomResult struct {
	Tag string `json:"tag"`
}

// DeleteUnderwritingRoom removes one room. versionID is optional (empty =
// the caller's newest version). 404 "room not found" when absent.
func (c *Client) DeleteUnderwritingRoom(ctx context.Context, token, propertyID, strategy, area, tag, versionID string) (*DeleteRoomResult, error) {
	q := strategyQuery(strategy)
	if versionID != "" {
		q.Set("versionId", versionID)
	}
	raw, err := c.uwDo(ctx, token, http.MethodDelete, roomPath(propertyID, area, tag), q, nil)
	if err != nil {
		return nil, err
	}
	var out DeleteRoomResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
