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

// Direct uploads of a property photo or a renovation render from a file the
// AI client holds: upload-url → the client PUTs the bytes to S3 → confirm.
// Client methods live in internal/reisearch/direct_upload.go.

const imageFormats = "jpg, jpeg, png, webp, gif, bmp, heic, heif, tif, tiff (up to 20 MB; the content must be a real image — no SVG, video or documents)"

// putInstructions is step 2, shared by both upload-url tools.
func putInstructions(confirmTool string) string {
	return "STEP 2 — YOU must send the file bytes yourself: an HTTP PUT of the raw file to upload_url with exactly the returned headers, e.g. `curl -X PUT -H \"Content-Type: <headers.Content-Type>\" --data-binary @<local file> \"<upload_url>\"`; do NOT send an Authorization header to it; it must answer 200; it expires after 15 minutes. " +
		"If you cannot make HTTP requests or read the user's file (e.g. a plain chat with no network access), don't call this tool: if the image is at a public URL use the URL tool instead, otherwise tell the user to upload it in the ReiSearch app. " +
		"STEP 3 — call " + confirmTool + " with the s3_key."
}

// directUploadError adds next-step hints to the direct upload failures.
func directUploadError(err error, uploadURLTool string) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	msg := apiErr.Message + " " + string(apiErr.Details)
	switch {
	case strings.Contains(msg, "No file found at s3_key"):
		return fmt.Errorf("%w — the file isn't in storage yet: PUT the bytes to upload_url (with the returned Content-Type header) and check it answered 200 before confirming. If the 15-minute upload_url expired, call %s again", err, uploadURLTool)
	case strings.Contains(msg, "not issued for this property"):
		return fmt.Errorf("%w — use the exact s3_key returned by %s for THIS propertyID", err, uploadURLTool)
	case strings.Contains(msg, "size limit"):
		return fmt.Errorf("%w — the image is over 20 MB; ask the user for a smaller version", err)
	case strings.Contains(msg, "not a supported image") || strings.Contains(msg, "empty file"):
		return fmt.Errorf("%w — the uploaded bytes aren't a valid image. Accepted: %s", err, imageFormats)
	case strings.Contains(msg, "unsupported file extension"):
		return fmt.Errorf("%w — accepted formats: %s", err, imageFormats)
	case strings.Contains(msg, "Content-Type"):
		return fmt.Errorf("%w — PUT the file again to the same upload_url (if not expired) with exactly the Content-Type from the upload-url headers, then confirm again", err)
	case strings.Contains(msg, "tag_type"):
		return fmt.Errorf("%w — tag_type must be a canonical room tag 'room:instance', e.g. 'kitchen:1' or 'bedroom:2'", err)
	case apiErr.Status == http.StatusNotFound:
		return fmt.Errorf("%w — check the propertyID", err)
	case apiErr.Status == http.StatusForbidden:
		return fmt.Errorf("%w — the user has no edit access to this property, or the propertyID is wrong", err)
	case apiErr.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited; wait a moment before trying again", err)
	}
	return err
}

func requireFileName(name, example string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("file_name is required, with its extension (e.g. '%s')", example)
	}
	return name, nil
}

// ---------------------------------------------------------------- get_property_image_upload_url

type GetPropertyImageUploadURLInput struct {
	PropertyID  string `json:"propertyID"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type,omitempty"`
}

func (h *PropertyHandler) GetPropertyImageUploadURL(ctx context.Context, req *mcp.CallToolRequest, input GetPropertyImageUploadURLInput) (*mcp.CallToolResult, *reisearch.DocumentUploadURL, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	fileName, err := requireFileName(input.FileName, "front.jpg")
	if err != nil {
		return nil, nil, err
	}
	result, err := h.client.CreatePropertyImageUploadURL(ctx, TokenFromContext(ctx), input.PropertyID, fileName, strings.TrimSpace(input.ContentType))
	if err != nil {
		return nil, nil, directUploadError(err, "get_property_image_upload_url")
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- confirm_property_image_upload

type ConfirmPropertyImageUploadInput struct {
	PropertyID string `json:"propertyID"`
	S3Key      string `json:"s3_key"`
	Type       string `json:"type,omitempty"`
}

func (h *PropertyHandler) ConfirmPropertyImageUpload(ctx context.Context, req *mcp.CallToolRequest, input ConfirmPropertyImageUploadInput) (*mcp.CallToolResult, *reisearch.UploadPropertyImageResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	s3Key := strings.TrimSpace(input.S3Key)
	if s3Key == "" {
		return nil, nil, fmt.Errorf("s3_key is required: the s3_key returned by get_property_image_upload_url")
	}
	if input.Type != "" && !slices.Contains(reisearch.PropertyImageTypes, input.Type) {
		return nil, nil, fmt.Errorf("type must be 'normal' or 'floor_plan'")
	}
	result, err := h.client.ConfirmPropertyImageUpload(ctx, TokenFromContext(ctx), input.PropertyID, s3Key, input.Type)
	if err != nil {
		return nil, nil, directUploadError(err, "get_property_image_upload_url")
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- get_renovation_image_upload_url

type GetRenovationImageUploadURLInput struct {
	PropertyID  string `json:"propertyID"`
	FileName    string `json:"file_name"`
	TagType     string `json:"tag_type"`
	ContentType string `json:"content_type,omitempty"`
}

func (h *PropertyHandler) GetRenovationImageUploadURL(ctx context.Context, req *mcp.CallToolRequest, input GetRenovationImageUploadURLInput) (*mcp.CallToolResult, *reisearch.DocumentUploadURL, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	fileName, err := requireFileName(input.FileName, "kitchen-render.png")
	if err != nil {
		return nil, nil, err
	}
	tagType := strings.TrimSpace(input.TagType)
	if tagType == "" {
		return nil, nil, fmt.Errorf("tag_type is required: the room this render is for, e.g. 'kitchen:1'")
	}
	result, err := h.client.CreateRenovationImageUploadURL(ctx, TokenFromContext(ctx), input.PropertyID, fileName, strings.TrimSpace(input.ContentType), tagType)
	if err != nil {
		return nil, nil, directUploadError(err, "get_renovation_image_upload_url")
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- confirm_renovation_image_upload

type ConfirmRenovationImageUploadInput struct {
	PropertyID string `json:"propertyID"`
	S3Key      string `json:"s3_key"`
}

func (h *PropertyHandler) ConfirmRenovationImageUpload(ctx context.Context, req *mcp.CallToolRequest, input ConfirmRenovationImageUploadInput) (*mcp.CallToolResult, *reisearch.SaveRenovationImageResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	s3Key := strings.TrimSpace(input.S3Key)
	if s3Key == "" {
		return nil, nil, fmt.Errorf("s3_key is required: the s3_key returned by get_renovation_image_upload_url")
	}
	result, err := h.client.ConfirmRenovationImageUpload(ctx, TokenFromContext(ctx), input.PropertyID, s3Key)
	if err != nil {
		return nil, nil, directUploadError(err, "get_renovation_image_upload_url")
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

func registerDirectUploadTools(server *mcp.Server, h *PropertyHandler) {

	mcp.AddTool(server, &mcp.Tool{Name: "get_property_image_upload_url", Description: "Step 1 of 3 to add a photo FILE (one you can read, not a URL) to a property's gallery. Requires 'propertyID' and 'file_name' (with extension, e.g. 'front.jpg'). Optional 'content_type' (defaults to the standard type for the extension). Accepted: " + imageFormats + ". Returns { upload_url, method, headers, s3_key, expires_at }. " +
		putInstructions("confirm_property_image_upload (optionally with type 'floor_plan'); nothing is added to the property until you confirm") +
		" If the photo is at a public URL, use upload_property_image instead (one call). For AI renovation renders use get_renovation_image_upload_url. Requires edit access to the property."}, h.GetPropertyImageUploadURL)

	mcp.AddTool(server, &mcp.Tool{Name: "confirm_property_image_upload", Description: "Step 3 of 3 to add a photo file: checks the file you PUT to the upload_url from get_property_image_upload_url and adds it to the property's gallery. Requires 'propertyID' and 's3_key' (exactly as returned for this property). Optional 'type': 'normal' (default) or 'floor_plan'. Safe to retry: confirming the same s3_key again returns the existing image without a duplicate. Returns { propertyID, image (s3_key, url — 15-minute view link, type, createdAt), imageCount }. Errors: 'No file found at s3_key' → the PUT didn't land, upload first; 'not a supported image' / size limit → the file isn't a valid image under 20 MB; Content-Type error → PUT again with the returned header. Requires edit access.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, h.ConfirmPropertyImageUpload)

	mcp.AddTool(server, &mcp.Tool{Name: "get_renovation_image_upload_url", Description: "Step 1 of 4 to save an AI renovation render from a FILE (one you can read, not a URL). Requires 'propertyID', 'file_name' (with extension, e.g. 'kitchen-render.png') and 'tag_type' (the canonical room tag the render is for, e.g. 'kitchen:1' — checked here, but you pass it again to submit). Optional 'content_type'. Accepted: " + imageFormats + ". Returns { upload_url, method, headers, s3_key, expires_at }. " +
		putInstructions("confirm_renovation_image_upload, then STEP 4 — submit_property_renovation with that s3_key, the same tag_type and a cost estimate") +
		" If the render is at a public URL, use save_renovation_image instead. For the property's own photos use get_property_image_upload_url."}, h.GetRenovationImageUploadURL)

	mcp.AddTool(server, &mcp.Tool{Name: "confirm_renovation_image_upload", Description: "Step 3 of 4 to save a renovation render file: checks the render you PUT to the upload_url from get_renovation_image_upload_url and returns { s3_key } for submit_property_renovation. Requires 'propertyID' and 's3_key' (exactly as returned for this property). IMPORTANT: this writes NOTHING to the property or the ledger — the render is only tracked once you call submit_property_renovation with this s3_key, the tag_type and a cost estimate. Safe to retry. Errors: 'No file found at s3_key' → the PUT didn't land, upload first; 'not a supported image' / size limit → not a valid image under 20 MB.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, h.ConfirmRenovationImageUpload)
}
