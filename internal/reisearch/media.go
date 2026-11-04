package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Property media: uploading and removing a property's photos. Listing lives
// in renovation.go (ListPropertyImages). Uses apiDo from underwriting.go so
// failures come back as *APIError and tools can add next-step hints.

// PropertyImageTypes are the image types the API accepts ("normal" is the default).
var PropertyImageTypes = []string{"normal", "floor_plan"}

// mediaProperty is the slice of the full Property object the media tools
// keep: the upload and remove endpoints return the whole property, which is
// large, so only its id and image keys are surfaced.
type mediaProperty struct {
	PropertyID string `json:"propertyID"`
	Images     []struct {
		S3Key string `json:"s3_key"`
	} `json:"images"`
}

func (p mediaProperty) imageKeys() []string {
	keys := make([]string, 0, len(p.Images))
	for _, img := range p.Images {
		keys = append(keys, img.S3Key)
	}
	return keys
}

// UploadPropertyImageResult is the trimmed upload response: the new image
// plus how many images the property now has.
type UploadPropertyImageResult struct {
	PropertyID string        `json:"propertyID"`
	Image      PropertyImage `json:"image"`
	ImageCount int           `json:"imageCount"`
}

// UploadPropertyImageFromURL adds a photo to a property by pointing the API at
// a public http(s) image URL, which it downloads (the JSON form of
// POST /property/{id}/image; the multipart form can't be driven from an MCP).
// imageType is "normal" or "floor_plan" ("" = API default "normal").
// Not idempotent: every success adds a new image with a new key.
func (c *Client) UploadPropertyImageFromURL(ctx context.Context, token, propertyID, imageURL, imageType string) (*UploadPropertyImageResult, error) {
	body := map[string]string{"image_url": imageURL}
	if imageType != "" {
		body["type"] = imageType
	}
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image"
	raw, err := c.apiDo(ctx, token, http.MethodPost, path, url.Values{}, body)
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

// RemovePropertyImagesResult is the trimmed remove response.
type RemovePropertyImagesResult struct {
	PropertyID     string   `json:"propertyID"`
	Removed        []string `json:"removed"`
	RemainingCount int      `json:"remainingCount"`
	RemainingKeys  []string `json:"remainingKeys"`
}

// RemovePropertyImages detaches images from a property by s3_key. All or
// nothing: if any key isn't on the property the API answers 404 and removes
// none. The stored files are kept; only the property's references go.
func (c *Client) RemovePropertyImages(ctx context.Context, token, propertyID string, keys []string) (*RemovePropertyImagesResult, error) {
	path := "/connect/v1/property/" + url.PathEscape(propertyID) + "/image"
	body := map[string][]string{"keys": keys}
	raw, err := c.apiDo(ctx, token, http.MethodDelete, path, url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var property mediaProperty
	if err := json.Unmarshal(raw, &property); err != nil {
		return nil, err
	}
	remaining := property.imageKeys()
	return &RemovePropertyImagesResult{
		PropertyID:     propertyID,
		Removed:        keys,
		RemainingCount: len(remaining),
		RemainingKeys:  remaining,
	}, nil
}
