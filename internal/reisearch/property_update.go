package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Partial property update: PATCH /connect/v1/property/{propertyID}.
// Nested objects merge field by field; lists replace the stored list whole;
// null, "", [] and {} are rejected (values can't be cleared); the address,
// images, tags, status and server-managed fields can't be sent. Optional
// If-Match (the property's updatedAt) refuses the write with 412 if the
// property changed since it was read. Uses apiDo from underwriting.go.

var (
	PropertyDealTypes          = []string{"cash", "creative"}
	PropertyCreativeStructures = []string{"subject_to", "hybrid", "seller_finance", "stack"}
)

// PropertyPatch mirrors the API's PropertyPatchDTO. Zero values are omitted
// (the API rejects "" and [] anyway); HoaDues is a pointer because 0 is a
// valid value.
type PropertyPatch struct {
	PropertyType      string                `json:"propertyType,omitempty"`
	Bedrooms          string                `json:"bedrooms,omitempty"`
	Bathrooms         string                `json:"bathrooms,omitempty"`
	YearBuilt         string                `json:"yearBuilt,omitempty"`
	PropertySize      int64                 `json:"propertySize,omitempty"`
	LotSize           int64                 `json:"lotSize,omitempty"`
	ListingPrice      float64               `json:"listingPrice,omitempty"`
	DealType          string                `json:"dealType,omitempty"`
	CreativeStructure string                `json:"creativeStructure,omitempty"`
	PropertyDetails   *PropertyDetailsPatch `json:"propertyDetails,omitempty"`
	AdditionalInfo    *AdditionalInfoPatch  `json:"additionalInfo,omitempty"`
	Occupancy         *OccupancyPatch       `json:"occupancy,omitempty"`
	CustomRoomTypes   []CustomRoomTypePatch `json:"customRoomTypes,omitempty"`
}

type PropertyDetailsPatch struct {
	APN              string   `json:"apn,omitempty"`
	HoaDues          *float64 `json:"hoaDues,omitempty"`
	HoaDuesFrequency string   `json:"hoaDuesFrequency,omitempty"`
	LegalDescription string   `json:"legalDescription,omitempty"`
	SubdivisionName  string   `json:"subdivisionName,omitempty"`
	WaterType        string   `json:"waterType,omitempty"`
	WasteType        string   `json:"wasteType,omitempty"`
	ElectricSupply   string   `json:"electricSupply,omitempty"`
}

type AdditionalInfoPatch struct {
	Interior       *InteriorPatch       `json:"interior,omitempty"`
	Others         *OthersPatch         `json:"others,omitempty"`
	UtilityDetails *UtilityDetailsPatch `json:"utilityDetails,omitempty"`
}

type InteriorPatch struct {
	Appliances []string `json:"appliances,omitempty"`
	Flooring   []string `json:"flooring,omitempty"`
}

type OthersPatch struct {
	ExteriorMaterial []string `json:"exteriorMaterial,omitempty"`
	IndoorFeatures   []string `json:"indoorFeatures,omitempty"`
	RoofAge          string   `json:"roofAge,omitempty"`
	Foundation       string   `json:"foundation,omitempty"`
}

type UtilityDetailsPatch struct {
	Cooling     []string `json:"cooling,omitempty"`
	Heating     []string `json:"heating,omitempty"`
	HeatingFuel []string `json:"heatingFuel,omitempty"`
}

type OccupancyPatch struct {
	OccupancyAtClosing string `json:"occupancyAtClosing,omitempty"`
	CurrentOccupancy   string `json:"currentOccupancy,omitempty"`
}

type CustomRoomTypePatch struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PatchBody marshals the patch and drops nested objects left empty (an AI
// sending "propertyDetails": {} would otherwise get a 400). Returns nil when
// nothing is left to send.
func (p PropertyPatch) PatchBody() (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	pruneEmptyObjects(body)
	if len(body) == 0 {
		return nil, nil
	}
	return body, nil
}

func pruneEmptyObjects(m map[string]any) {
	for k, v := range m {
		if child, ok := v.(map[string]any); ok {
			pruneEmptyObjects(child)
			if len(child) == 0 {
				delete(m, k)
			}
		}
	}
}

// editablePropertyKeys are the property attributes surfaced after an update:
// the ones this endpoint can change, so the caller sees their new values
// without the (large) rest of the property.
var editablePropertyKeys = []string{
	"propertyType", "bedrooms", "bathrooms", "yearBuilt", "propertySize", "lotSize",
	"listingPrice", "dealType", "creativeStructure", "propertyDetails",
	"additionalInfo", "occupancy", "customRoomTypes",
}

// UpdatePropertyResult is the trimmed PATCH response.
type UpdatePropertyResult struct {
	PropertyID    string         `json:"propertyID"`
	DryRun        bool           `json:"dryRun"`
	ChangedFields []string       `json:"changedFields"`
	Version       int64          `json:"version,omitempty"`
	Property      map[string]any `json:"property"`
}

// UpdateProperty sends a partial update. expectedVersion (the property's
// updatedAt, epoch ms) goes in If-Match when non-zero.
func (c *Client) UpdateProperty(ctx context.Context, token, propertyID string, body map[string]any, expectedVersion int64, dryRun bool) (*UpdatePropertyResult, error) {
	q := url.Values{}
	if dryRun {
		q.Set("dryRun", "true")
	}
	var headers map[string]string
	if expectedVersion != 0 {
		headers = map[string]string{"If-Match": strconv.Quote(strconv.FormatInt(expectedVersion, 10))}
	}
	path := "/connect/v1/property/" + url.PathEscape(propertyID)
	raw, err := c.apiDoWithHeaders(ctx, token, http.MethodPatch, path, q, body, headers)
	if err != nil {
		return nil, err
	}
	var data struct {
		Property      map[string]any `json:"property"`
		ChangedFields []string       `json:"changedFields"`
		DryRun        bool           `json:"dryRun"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	out := &UpdatePropertyResult{
		PropertyID:    propertyID,
		DryRun:        data.DryRun,
		ChangedFields: data.ChangedFields,
		Property:      map[string]any{},
	}
	if out.ChangedFields == nil {
		out.ChangedFields = []string{}
	}
	for _, k := range editablePropertyKeys {
		if v, ok := data.Property[k]; ok && v != nil {
			out.Property[k] = v
		}
	}
	if v, ok := data.Property["updatedAt"].(float64); ok {
		out.Version = int64(v)
	}
	return out, nil
}
