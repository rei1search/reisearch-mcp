package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// ListPropertyImagesInput drives list_property_images. PropertyID is required.
type ListPropertyImagesInput struct {
	PropertyID string `json:"propertyID"`
}

func (h *PropertyHandler) ListPropertyImages(ctx context.Context, req *mcp.CallToolRequest, input ListPropertyImagesInput) (*mcp.CallToolResult, *reisearch.PropertyImagesResult, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}

	result, err := h.client.ListPropertyImages(ctx, token, input.PropertyID)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// TagPropertyImagesInput drives tag_property_images. Images needs at least one
// entry; each entry's s3_key comes from list_property_images and tag_type is
// the canonical room tag (`root:instance`, e.g. "kitchen:1").
type TagPropertyImagesInput struct {
	PropertyID string                    `json:"propertyID"`
	Images     []reisearch.ImageTagEntry `json:"images"`
}

func (h *PropertyHandler) TagPropertyImages(ctx context.Context, req *mcp.CallToolRequest, input TagPropertyImagesInput) (*mcp.CallToolResult, *reisearch.TagImagesResult, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if len(input.Images) == 0 {
		return nil, nil, fmt.Errorf("images requires at least one entry")
	}
	for i, img := range input.Images {
		if img.S3Key == "" {
			return nil, nil, fmt.Errorf("images[%d].s3_key is required", i)
		}
		if img.TagType == "" {
			return nil, nil, fmt.Errorf("images[%d].tag_type is required", i)
		}
	}

	result, err := h.client.TagPropertyImages(ctx, token, input.PropertyID, input.Images)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// SubmitPropertyRenovationInput drives submit_property_renovation. The s3_key
// must reference an ALREADY-UPLOADED render (minted by the app-side upload —
// there is deliberately no upload tool); never fabricate one.
type SubmitPropertyRenovationInput struct {
	PropertyID string                       `json:"propertyID"`
	S3Key      string                       `json:"s3_key"`
	TagType    string                       `json:"tag_type"`
	Estimate   reisearch.RenovationEstimate `json:"estimate"`
}

func (h *PropertyHandler) SubmitPropertyRenovation(ctx context.Context, req *mcp.CallToolRequest, input SubmitPropertyRenovationInput) (*mcp.CallToolResult, map[string]interface{}, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if input.S3Key == "" {
		return nil, nil, fmt.Errorf("s3_key is required")
	}
	if input.TagType == "" {
		return nil, nil, fmt.Errorf("tag_type is required")
	}

	result, err := h.client.SubmitPropertyRenovation(ctx, token, input.PropertyID, input.S3Key, input.TagType, input.Estimate)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

// GetRenovationLedgerInput drives get_renovation_ledger. Sections is optional;
// each value must be one of accepted, before, after (empty = all three).
type GetRenovationLedgerInput struct {
	PropertyID string   `json:"propertyID"`
	Sections   []string `json:"sections,omitempty"`
}

func (h *PropertyHandler) GetRenovationLedger(ctx context.Context, req *mcp.CallToolRequest, input GetRenovationLedgerInput) (*mcp.CallToolResult, *reisearch.RenovationLedger, error) {
	token := TokenFromContext(ctx)

	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}

	result, err := h.client.GetRenovationLedger(ctx, token, input.PropertyID, input.Sections)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

func registerRenovationTools(server *mcp.Server, h *PropertyHandler) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_property_images", Description: "List a property's images, each with a short-lived view URL and its renovation tag/status. Requires 'propertyID'. Use this to show the user their photos and to get the 's3_key' values needed to tag them for renovation. The 'url' on each image is a temporary download link (no auth needed) — don't cache it; 's3_key' is the stable identifier. Returns a '404' if the property doesn't exist."}, h.ListPropertyImages)
	mcp.AddTool(server, &mcp.Tool{Name: "tag_property_images", Description: "Tag one or more of a property's photos as renovation 'before' shots for the current user. Requires 'propertyID' and 'images' (each with 's3_key' — from list_property_images — and 'tag_type', the canonical room tag like 'kitchen:1' or 'bedroom:2'). This records per-user renovation state; it does NOT change the shared property. Errors: 'image not found on property', 'invalid tag_type', or 'image key required' → check the s3_key and tag. The tag_type you use here is the same one that links to underwriting rooms later."}, h.TagPropertyImages)
	mcp.AddTool(server, &mcp.Tool{Name: "submit_property_renovation", Description: "Submit an AI-rendered image plus its cost estimate into the property's underwriting for the current user. This maps the estimate to a repairRenovation room (by 'tag_type') and writes it to underwriting, then records the render on the ledger's 'after'/'accepted' buckets. Requires 'propertyID', 's3_key' (the render's key — this comes from an app-side upload; you cannot generate or upload a render through these tools, so only call this when you already have an s3_key from the ledger, the property images, or the user), 'tag_type', and 'estimate' (cost breakdown: lineItems with per-room name/materialCost/laborCost/total, an optional summary of totals, and optional finishGrade/rehabLevel). Returns the underwriting room that was written. On a 500 ('failed to submit renovation to underwriting') the write may not have landed — it's safe to retry the same s3_key + tag_type. IMPORTANT: don't fabricate an s3_key; if you don't have one, tell the user the render must be created/uploaded in the ReiSearch app first."}, h.SubmitPropertyRenovation)
	mcp.AddTool(server, &mcp.Tool{Name: "get_renovation_ledger", Description: "Read the current user's renovation ledger for a property — which photos are tagged 'before', which renders were submitted ('after'), and the live render per room ('accepted', keyed by tag_type). Requires 'propertyID'; optionally filter with 'sections' (an array holding any of 'accepted', 'before', 'after'; omit it for all three — any other value fails with a 400). This is per-user state — do not present it as the whole property's. A property with no renovation activity yet returns empty sections, not an error."}, h.GetRenovationLedger)
}
