package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Direct-to-S3 uploads for a property's photos and renovation renders:
// upload-url (presigned PUT, 15 minutes) → the caller PUTs the bytes →
// confirm. Same shape as the document flow in documents.go; the upload-url
// response reuses DocumentUploadURL. Uses apiDo so failures are *APIError.

func imagePath(propertyID string) string {
	return "/connect/v1/property/" + url.PathEscape(propertyID) + "/image"
}

func uploadURLBody(fileName, contentType string) map[string]string {
	body := map[string]string{"file_name": fileName}
	if contentType != "" {
		body["content_type"] = contentType
	}
	return body
}

func (c *Client) postUploadURL(ctx context.Context, token, path string, body map[string]string) (*DocumentUploadURL, error) {
	raw, err := c.apiDo(ctx, token, http.MethodPost, path, url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var out DocumentUploadURL
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePropertyImageUploadURL mints PROPERTY_IMAGES/<id>/<uuid><ext> and
// returns a presigned PUT for it. Nothing is attached until confirm.
func (c *Client) CreatePropertyImageUploadURL(ctx context.Context, token, propertyID, fileName, contentType string) (*DocumentUploadURL, error) {
	return c.postUploadURL(ctx, token, imagePath(propertyID)+"/upload-url", uploadURLBody(fileName, contentType))
}

// ConfirmPropertyImageUpload checks the object at s3Key and appends it to the
// property's images, like the URL upload. Idempotent: an already-attached key
// returns the existing image. imageType "" = API default "normal".
func (c *Client) ConfirmPropertyImageUpload(ctx context.Context, token, propertyID, s3Key, imageType string) (*UploadPropertyImageResult, error) {
	body := map[string]string{"s3_key": s3Key}
	if imageType != "" {
		body["type"] = imageType
	}
	raw, err := c.apiDo(ctx, token, http.MethodPost, imagePath(propertyID)+"/confirm", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var data struct {
		Image    PropertyImage `json:"image"`
		Property mediaProperty `json:"property"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &UploadPropertyImageResult{
		PropertyID: propertyID,
		Image:      data.Image,
		ImageCount: len(data.Property.Images),
	}, nil
}

// CreateRenovationImageUploadURL mints
// properties/<id>/generated/<uuid>/img-<nano><ext> and returns a presigned PUT.
// tagType is validated but not stored at this step.
func (c *Client) CreateRenovationImageUploadURL(ctx context.Context, token, propertyID, fileName, contentType, tagType string) (*DocumentUploadURL, error) {
	body := uploadURLBody(fileName, contentType)
	body["tag_type"] = tagType
	return c.postUploadURL(ctx, token, imagePath(propertyID)+"/renovation/upload-url", body)
}

// ConfirmRenovationImageUpload checks the render at s3Key and returns the key
// to pass to SubmitPropertyRenovation. Writes nothing to the property.
func (c *Client) ConfirmRenovationImageUpload(ctx context.Context, token, propertyID, s3Key string) (*SaveRenovationImageResult, error) {
	raw, err := c.apiDo(ctx, token, http.MethodPost, imagePath(propertyID)+"/renovation/confirm", url.Values{}, map[string]string{"s3_key": s3Key})
	if err != nil {
		return nil, err
	}
	var out SaveRenovationImageResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
