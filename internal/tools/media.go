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

// Property media tools: add and remove a property's photos. Client methods
// live in internal/reisearch/media.go; list_property_images is in renovation.go.

// mediaError adds a next-step hint to known media API failures.
func mediaError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case strings.Contains(apiErr.Message, "could not be fetched"):
		return fmt.Errorf("%w — the server couldn't download that URL (unreachable, refused, timed out, not found, or over 20 MB). Ask the user for a public, direct link to the image file", err)
	case apiErr.Status == http.StatusForbidden:
		return fmt.Errorf("%w — the user has no edit access to this property, OR the propertyID doesn't exist (the API answers 403 for both); check the propertyID", err)
	case apiErr.Status == http.StatusNotFound && strings.Contains(apiErr.Message, "image not found on property"):
		return fmt.Errorf("%w — NOTHING was removed (all or nothing). Call list_property_images, then retry with only keys that are on the property", err)
	case apiErr.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited; wait a moment before trying again", err)
	}
	return err
}

// ---------------------------------------------------------------- upload_property_image

type UploadPropertyImageInput struct {
	PropertyID string `json:"propertyID"`
	ImageURL   string `json:"image_url"`
	Type       string `json:"type,omitempty"`
}

func (h *PropertyHandler) UploadPropertyImage(ctx context.Context, req *mcp.CallToolRequest, input UploadPropertyImageInput) (*mcp.CallToolResult, *reisearch.UploadPropertyImageResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	imageURL := strings.TrimSpace(input.ImageURL)
	if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
		return nil, nil, fmt.Errorf("image_url must be a public http(s) link to the image (not a local file path or data: URI)")
	}
	if input.Type != "" && !slices.Contains(reisearch.PropertyImageTypes, input.Type) {
		return nil, nil, fmt.Errorf("type must be 'normal' or 'floor_plan'")
	}

	result, err := h.client.UploadPropertyImageFromURL(ctx, TokenFromContext(ctx), input.PropertyID, imageURL, input.Type)
	if err != nil {
		var apiErr *reisearch.APIError
		if !errors.As(err, &apiErr) {
			return nil, nil, fmt.Errorf("%w — the image may still have been added; call list_property_images before retrying, or you may create a duplicate", err)
		}
		return nil, nil, mediaError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- remove_property_images

type RemovePropertyImagesInput struct {
	PropertyID string   `json:"propertyID"`
	Keys       []string `json:"keys"`
}

func (h *PropertyHandler) RemovePropertyImages(ctx context.Context, req *mcp.CallToolRequest, input RemovePropertyImagesInput) (*mcp.CallToolResult, *reisearch.RemovePropertyImagesResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	// Trim and de-duplicate, as the API does, so the result lists what was sent.
	keys := []string{}
	for _, k := range input.Keys {
		if k = strings.TrimSpace(k); k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("keys is required: at least one 's3_key' from list_property_images")
	}

	result, err := h.client.RemovePropertyImages(ctx, TokenFromContext(ctx), input.PropertyID, keys)
	if err != nil {
		return nil, nil, mediaError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

func registerMediaTools(server *mcp.Server, h *PropertyHandler) {
	destructive := true

	mcp.AddTool(server, &mcp.Tool{Name: "upload_property_image", Description: "Add a photo to a property's gallery by giving a public URL to the image: the server downloads and stores it. Requires 'propertyID' and 'image_url' (a public http/https DIRECT link to the image file — NOT a local file path, a data: URI, or a web page that merely shows the image). Optional 'type': 'normal' (default) or 'floor_plan'. Accepted formats: jpg, jpeg, png, webp, gif, bmp, heic, heif, tif, tiff, up to 20 MB; the content is checked, so a non-image fails (SVG, video and documents are not accepted). For an image FILE you can read (not a URL), use get_property_image_upload_url instead; if you can do neither, the user can upload it in the ReiSearch app. This is for the property's own photos; for AI renovation renders use save_renovation_image instead. Requires edit access to the property. Returns { propertyID, image (s3_key, url, type, createdAt), imageCount }. Not idempotent: each call adds another copy, so don't retry blindly — call list_property_images first. Errors: 'image_url could not be fetched' → ask for a working public direct image link; 403 → no edit access OR the property doesn't exist."}, h.UploadPropertyImage)

	mcp.AddTool(server, &mcp.Tool{Name: "remove_property_images", Description: "Remove one or more photos from a property's gallery. CONFIRM with the user first — name or describe the photos you're about to remove. Requires 'propertyID' and 'keys' (an array of 's3_key' values from list_property_images). All or nothing: if ANY key isn't on the property, NOTHING is removed and the error names the bad key — list the images again and retry with valid keys. This only detaches the photos from the property; the stored files are kept. Requires edit access (403 means no edit access OR the property doesn't exist). Returns { propertyID, removed, remainingCount, remainingKeys }.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}, h.RemovePropertyImages)
}
