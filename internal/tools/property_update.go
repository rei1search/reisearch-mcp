package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// update_property: partial update of a property's details. Client method in
// internal/reisearch/property_update.go.

type UpdatePropertyInput struct {
	PropertyID      string `json:"propertyID"`
	ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	DryRun          bool   `json:"dryRun,omitempty"`
	reisearch.PropertyPatch
}

func updatePropertyError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case http.StatusBadRequest:
		return fmt.Errorf("%w — NOTHING was saved (all or nothing). Fix every field listed in details (field + code) and retry; don't resend the same body. address_immutable → the address can't be changed; null_not_allowed / empty values → omit the field, values can't be cleared; unknown fields → only the documented fields can be updated", err)
	case http.StatusPreconditionFailed, http.StatusConflict:
		return fmt.Errorf("%w — the property changed since you read it; nothing was saved. Call get_property_details again, check the change still makes sense, and retry with the new property_detail.updatedAt as expectedVersion", err)
	case http.StatusNotFound:
		return fmt.Errorf("%w — no property with that propertyID", err)
	case http.StatusForbidden:
		return fmt.Errorf("%w — the user doesn't have edit access to this property", err)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited; wait a moment before trying again", err)
	}
	return err
}

func (h *PropertyHandler) UpdateProperty(ctx context.Context, req *mcp.CallToolRequest, input UpdatePropertyInput) (*mcp.CallToolResult, *reisearch.UpdatePropertyResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	p := input.PropertyPatch
	if p.DealType != "" && !slices.Contains(reisearch.PropertyDealTypes, p.DealType) {
		return nil, nil, fmt.Errorf("dealType must be 'cash' or 'creative'")
	}
	if p.CreativeStructure != "" && !slices.Contains(reisearch.PropertyCreativeStructures, p.CreativeStructure) {
		return nil, nil, fmt.Errorf("creativeStructure must be one of: %s", strings.Join(reisearch.PropertyCreativeStructures, ", "))
	}
	if p.DealType == "cash" && p.CreativeStructure != "" {
		return nil, nil, fmt.Errorf("don't send creativeStructure with dealType 'cash' — switching to cash clears it automatically")
	}
	body, err := p.PatchBody()
	if err != nil {
		return nil, nil, err
	}
	if body == nil {
		return nil, nil, fmt.Errorf("nothing to update: send at least one field to change (e.g. listingPrice, bedrooms, propertyDetails.hoaDues)")
	}

	result, err := h.client.UpdateProperty(ctx, TokenFromContext(ctx), input.PropertyID, body, input.ExpectedVersion, input.DryRun)
	if err != nil {
		return nil, nil, updatePropertyError(err)
	}
	return nil, result, nil
}

const updatePropertyDescription = "Update some of a property's details. Send ONLY the fields to change; everything else is kept. CONFIRM the change with the user first. " +
	"Recommended flow: call get_property_details, pass its property_detail.updatedAt as 'expectedVersion' (the update is refused with 412 if someone changed the property since), and pass the returned 'version' as expectedVersion on the next update. 'dryRun': true validates and shows changedFields without saving. " +
	"Fields: propertyType; bedrooms (STRING whole number, e.g. \"3\"); bathrooms (STRING, e.g. \"2.5\"); yearBuilt (STRING 4-digit year); propertySize and lotSize (square feet, integers > 0); listingPrice (number > 0); dealType ('cash' or 'creative'); creativeStructure ('subject_to', 'hybrid', 'seller_finance', 'stack' — only for creative deals); " +
	"propertyDetails { apn, hoaDues (number ≥ 0), hoaDuesFrequency, legalDescription, subdivisionName, waterType, wasteType, electricSupply }; " +
	"additionalInfo { interior { appliances[], flooring[] }, others { exteriorMaterial[], indoorFeatures[], roofAge, foundation }, utilityDetails { cooling[], heating[], heatingFuel[] } }; " +
	"occupancy { occupancyAtClosing, currentOccupancy }; customRoomTypes [{ id, label }]. " +
	"Rules: nested objects MERGE (other keys are kept), but a list REPLACES the stored list entirely — to add one item, read the current list and send the full new list (no duplicates). Values can't be cleared (null/empty is rejected). The ADDRESS can never be changed, and images, tags and status have their own tools. Switching dealType to 'cash' clears creativeStructure. " +
	"Returns { propertyID, dryRun, changedFields, version, property (the editable fields after the update) }. changedFields [] means nothing changed — repeating the same update is safe. Validation is all or nothing: a 400 lists every bad field. Requires edit access."

func registerPropertyUpdateTool(server *mcp.Server, h *PropertyHandler) {
	idempotent := true
	mcp.AddTool(server, &mcp.Tool{Name: "update_property", Description: updatePropertyDescription, Annotations: &mcp.ToolAnnotations{IdempotentHint: idempotent}}, h.UpdateProperty)
}
