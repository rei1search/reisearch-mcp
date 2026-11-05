package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Property documents: listing, and the direct-to-S3 upload flow
// (upload-url → caller PUTs the bytes → confirm). The multipart
// POST /property/{id}/document can't be driven from an MCP tool call, so it
// isn't wrapped. There is no document delete endpoint. Uses apiDo from
// underwriting.go so failures come back as *APIError.

// PropertyDocumentCategories mirrors allowedDocumentCategories in the public
// API (internal/domains/properties/repository/property_documents_repo.go).
// Any value outside this list is rejected with 400.
var PropertyDocumentCategories = []string{
	"addendums", "additionalContract", "additionalDocuments",
	"additionalLiens", "amendments", "assignmentContractAToB",
	"assignmentContractBToC", "aToBEmdReceipt", "contractorBids",
	"countyTaxStatement", "customDocuments", "deathCertificate",
	"endBuyerEmdReceipt", "executedContract", "floorPlanCubiCasa",
	"helocStatement", "hoaAndCcr", "inspectionReport",
	"insurance", "jvAgreement", "mortgagePayoff",
	"mortgageStatement", "nonDisclosureAgreement", "profitAndLoss",
	"rentRoll", "sellerDisclosures", "solar",
	"titleWireInstructions", "trailing12Months",
}

// PropertyDocument is one document as the API returns it, with a presigned
// view URL valid for 15 minutes.
type PropertyDocument struct {
	Category   string `json:"category"`
	Name       string `json:"name"`
	S3Key      string `json:"s3_key"`
	URL        string `json:"url,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	GroupKey   string `json:"group_key,omitempty"`
	GroupLabel string `json:"group_label,omitempty"`
}

// ListPropertyDocumentsResult is the tool output (an object, never a bare slice).
type ListPropertyDocumentsResult struct {
	PropertyID string             `json:"propertyID"`
	Category   string             `json:"category,omitempty"`
	Count      int                `json:"count"`
	Documents  []PropertyDocument `json:"documents"`
}

func documentPath(propertyID string) string {
	return "/connect/v1/property/" + url.PathEscape(propertyID) + "/document"
}

// ListPropertyDocuments returns every document on the property, or only one
// category's when category is non-empty.
func (c *Client) ListPropertyDocuments(ctx context.Context, token, propertyID, category string) (*ListPropertyDocumentsResult, error) {
	q := url.Values{}
	if category != "" {
		q.Set("category", category)
	}
	raw, err := c.apiDo(ctx, token, http.MethodGet, documentPath(propertyID), q, nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Documents []PropertyDocument `json:"documents"`
	}
	if !isEmptyData(raw) {
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
	}
	if data.Documents == nil {
		data.Documents = []PropertyDocument{}
	}
	return &ListPropertyDocumentsResult{
		PropertyID: propertyID,
		Category:   category,
		Count:      len(data.Documents),
		Documents:  data.Documents,
	}, nil
}

// DocumentUploadURL tells the caller how to PUT the file to S3 (step 1 of the
// direct upload). Nothing is recorded on the property until confirm.
type DocumentUploadURL struct {
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	S3Key     string            `json:"s3_key"`
	ExpiresAt string            `json:"expires_at"`
}

// CreatePropertyDocumentUploadURL mints a key under the property and returns a
// presigned S3 PUT URL (15 minutes). contentType "" = the API's default for
// the file's extension.
func (c *Client) CreatePropertyDocumentUploadURL(ctx context.Context, token, propertyID, fileName, contentType string) (*DocumentUploadURL, error) {
	body := map[string]string{"file_name": fileName}
	if contentType != "" {
		body["content_type"] = contentType
	}
	raw, err := c.apiDo(ctx, token, http.MethodPost, documentPath(propertyID)+"/upload-url", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var out DocumentUploadURL
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConfirmPropertyDocumentUpload records an object already PUT to the upload
// URL on the property's category list (step 3). Idempotent for the same key
// and category; 409 if the key is recorded under another category.
func (c *Client) ConfirmPropertyDocumentUpload(ctx context.Context, token, propertyID, s3Key, fileName, category string) (*PropertyDocument, error) {
	body := map[string]string{"s3_key": s3Key, "file_name": fileName, "category": category}
	raw, err := c.apiDo(ctx, token, http.MethodPost, documentPath(propertyID)+"/confirm", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var out PropertyDocument
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
