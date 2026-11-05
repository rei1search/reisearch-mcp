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

// Property document tools: list a property's documents, and the two API halves
// of the direct upload (upload-url, confirm). The PUT of the file bytes in
// between is done by the AI client itself — a tool call can't carry a file.
// Client methods live in internal/reisearch/documents.go.

var documentCategoryList = strings.Join(reisearch.PropertyDocumentCategories, ", ")

func validateDocumentCategory(category string, required bool) error {
	if category == "" {
		if required {
			return fmt.Errorf("category is required: one of %s (use 'additionalDocuments' if none fits)", documentCategoryList)
		}
		return nil
	}
	if !slices.Contains(reisearch.PropertyDocumentCategories, category) {
		return fmt.Errorf("unknown category %q: must be exactly one of %s. Don't invent names — if none fits, use 'additionalDocuments'", category, documentCategoryList)
	}
	return nil
}

// documentError adds a next-step hint to known document API failures.
func documentError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	details := string(apiErr.Details)
	switch {
	case strings.Contains(apiErr.Message, "No file found at s3_key"):
		return fmt.Errorf("%w — the file isn't in storage yet: PUT the bytes to upload_url (with the returned Content-Type header) and check it answered 200 before confirming. If the 15-minute upload_url expired, call get_property_document_upload_url again", err)
	case strings.Contains(details, "not issued for this property"):
		return fmt.Errorf("%w — use the exact s3_key returned by get_property_document_upload_url for THIS propertyID", err)
	case strings.Contains(details, "unsupported file type") || strings.Contains(details, "not allowed for"):
		return fmt.Errorf("%w — that file type isn't accepted. Allowed: pdf, txt, rtf, md, csv, doc(x), xls(x), ppt(x), odt/ods/odp, common images (jpg, png, gif, bmp, tif, heic, webp, svg), RAW photos, and video (mp4, mov, avi, mkv, wmv, webm). If the stored type was wrong, PUT the file again with the Content-Type from the upload-url headers, then confirm", err)
	case apiErr.Status == http.StatusConflict:
		return fmt.Errorf("%w — this file is already recorded on the property under the category named in the details; nothing was added. Use list_property_documents to see it", err)
	case apiErr.Status == http.StatusForbidden:
		return fmt.Errorf("%w — the user lacks access to this property (viewing documents needs view access, uploading needs edit access), or the propertyID is wrong", err)
	case apiErr.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited; wait a moment before trying again", err)
	}
	return err
}

// ---------------------------------------------------------------- list_property_documents

type ListPropertyDocumentsInput struct {
	PropertyID string `json:"propertyID"`
	Category   string `json:"category,omitempty"`
}

func (h *PropertyHandler) ListPropertyDocuments(ctx context.Context, req *mcp.CallToolRequest, input ListPropertyDocumentsInput) (*mcp.CallToolResult, *reisearch.ListPropertyDocumentsResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	category := strings.TrimSpace(input.Category)
	if err := validateDocumentCategory(category, false); err != nil {
		return nil, nil, err
	}
	result, err := h.client.ListPropertyDocuments(ctx, TokenFromContext(ctx), input.PropertyID, category)
	if err != nil {
		return nil, nil, documentError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- get_property_document_upload_url

type GetPropertyDocumentUploadURLInput struct {
	PropertyID  string `json:"propertyID"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type,omitempty"`
}

func (h *PropertyHandler) GetPropertyDocumentUploadURL(ctx context.Context, req *mcp.CallToolRequest, input GetPropertyDocumentUploadURLInput) (*mcp.CallToolResult, *reisearch.DocumentUploadURL, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	fileName := strings.TrimSpace(input.FileName)
	if fileName == "" {
		return nil, nil, fmt.Errorf("file_name is required, with its extension (e.g. 'inspection.pdf')")
	}
	result, err := h.client.CreatePropertyDocumentUploadURL(ctx, TokenFromContext(ctx), input.PropertyID, fileName, strings.TrimSpace(input.ContentType))
	if err != nil {
		return nil, nil, documentError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- confirm_property_document_upload

type ConfirmPropertyDocumentUploadInput struct {
	PropertyID string `json:"propertyID"`
	S3Key      string `json:"s3_key"`
	FileName   string `json:"file_name"`
	Category   string `json:"category"`
}

func (h *PropertyHandler) ConfirmPropertyDocumentUpload(ctx context.Context, req *mcp.CallToolRequest, input ConfirmPropertyDocumentUploadInput) (*mcp.CallToolResult, *reisearch.PropertyDocument, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	s3Key := strings.TrimSpace(input.S3Key)
	if s3Key == "" {
		return nil, nil, fmt.Errorf("s3_key is required: the s3_key returned by get_property_document_upload_url")
	}
	fileName := strings.TrimSpace(input.FileName)
	if fileName == "" {
		return nil, nil, fmt.Errorf("file_name is required: the name to show for the document, with its extension")
	}
	category := strings.TrimSpace(input.Category)
	if err := validateDocumentCategory(category, true); err != nil {
		return nil, nil, err
	}
	result, err := h.client.ConfirmPropertyDocumentUpload(ctx, TokenFromContext(ctx), input.PropertyID, s3Key, fileName, category)
	if err != nil {
		return nil, nil, documentError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

func registerDocumentTools(server *mcp.Server, h *PropertyHandler) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_property_documents", Description: "List the documents attached to a property (contracts, inspection reports, statements, bids, etc.). Requires 'propertyID'. Optional 'category' to return only one category: one of " + documentCategoryList + ". Returns { propertyID, category, count, documents: [{ category, name, s3_key, url, updated_at, group_key?, group_label? }] }. 'url' is a view/download link valid for 15 MINUTES — share it with the user right away and call this tool again for a fresh one rather than reusing an old link. group_key/group_label appear only on documents in a custom group created in the ReiSearch app. A property with no documents returns an empty list. Requires view access to the property."}, h.ListPropertyDocuments)

	mcp.AddTool(server, &mcp.Tool{Name: "get_property_document_upload_url", Description: "Step 1 of 3 to attach a document file to a property. Requires 'propertyID' and 'file_name' (with extension, e.g. 'inspection.pdf'). Optional 'content_type' (defaults to the standard type for the extension). Returns { upload_url, method, headers, s3_key, expires_at }. STEP 2 — YOU must send the file bytes yourself: an HTTP PUT of the raw file to upload_url with exactly the returned headers, e.g. `curl -X PUT -H \"Content-Type: <headers.Content-Type>\" --data-binary @<local file> \"<upload_url>\"`; do NOT send an Authorization header to it; it must answer 200; it expires after 15 minutes. If you cannot make HTTP requests or read the user's file (e.g. you're in a plain chat), don't call this tool — tell the user to upload the document in the ReiSearch app. STEP 3 — call confirm_property_document_upload with the s3_key, file_name and a category; nothing appears on the property until you confirm. Allowed types: pdf, txt, rtf, md, csv, doc(x), xls(x), ppt(x), odt/ods/odp, images (jpg, png, gif, bmp, tif, heic, webp, svg), RAW photos, video (mp4, mov, avi, mkv, wmv, webm). Documents can't be deleted through the API, so confirm the right file and property with the user first. Requires edit access to the property."}, h.GetPropertyDocumentUploadURL)

	mcp.AddTool(server, &mcp.Tool{Name: "confirm_property_document_upload", Description: "Step 3 of 3 to attach a document: records a file you already PUT to the upload_url from get_property_document_upload_url. Requires 'propertyID', 's3_key' (exactly as returned for this property), 'file_name' (name shown for the document, with extension) and 'category' — exactly one of: " + documentCategoryList + ". Pick the closest; if none fits use 'additionalDocuments' — never invent or pluralize a name (e.g. 'helocStatements', 'misc' are rejected). Examples: HELOC statement → helocStatement; inspection → inspectionReport; CubiCasa floor plan → floorPlanCubiCasa; signed purchase contract → executedContract. Safe to retry: confirming the same s3_key and category again returns the existing document without a duplicate. Returns the document { category, name, s3_key, url (15-minute view link), updated_at }. Errors: 'No file found at s3_key' → the PUT didn't land, upload first; 409 → already recorded under another category. Requires edit access."}, h.ConfirmPropertyDocumentUpload)
}
