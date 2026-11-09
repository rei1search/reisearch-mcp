package tools

// Live end-to-end test of the direct image/render uploads and the document
// URL upload against the REAL public API (shared harness, see
// underwriting_live_test.go).
//
// Leaves behind: one document (there is no document delete endpoint) and two
// unsubmitted render files in storage. The test photo is removed again. Needs
// an extra opt-in:
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<throwaway property id> \
//	REISEARCH_LIVE_UPLOADS=1 \
//	go test ./internal/tools/ -run LiveDirectUploads -v

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// putUpload does step 2 the way an AI client would: PUT the bytes to the
// presigned URL with the returned Content-Type, no auth header.
func putUpload(t *testing.T, up liveResult, body []byte) {
	t.Helper()
	uploadURL, _ := up.data["upload_url"].(string)
	contentType, _ := dig(up.data, "headers", "Content-Type").(string)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("FAIL PUT to upload_url: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FAIL PUT to upload_url: status %d", resp.StatusCode)
	}
}

func TestLiveDirectUploads(t *testing.T) {
	if os.Getenv("REISEARCH_LIVE_UPLOADS") != "1" {
		t.Skip("set REISEARCH_LIVE_UPLOADS=1 to run (adds a permanent test document)")
	}
	h := newLiveHarness(t)
	pngBytes := testPNG(t)
	foreign := "00000000-0000-0000-0000-000000000000"

	t.Run("property image", func(t *testing.T) {
		h.failsWith("upload-url: no file_name", h.call("get_property_image_upload_url", map[string]any{"file_name": " "}), "file_name is required")
		h.failsWith("confirm: bad type", h.call("confirm_property_image_upload", map[string]any{"s3_key": "x", "type": "panorama"}), "'normal' or 'floor_plan'")
		h.failsWith("upload-url: bad extension", h.call("get_property_image_upload_url", map[string]any{"file_name": "notes.pdf"}), "accepted formats")
		h.failsWith("confirm: foreign key", h.call("confirm_property_image_upload", map[string]any{"s3_key": "PROPERTY_IMAGES/" + foreign + "/x.png"}), "THIS propertyID")

		up := h.call("get_property_image_upload_url", map[string]any{"file_name": "mcp-test.png"})
		if !h.ok("upload-url", up) {
			t.FailNow()
		}
		key, _ := up.data["s3_key"].(string)
		if !strings.HasPrefix(key, "PROPERTY_IMAGES/"+h.property+"/") || !strings.HasSuffix(key, ".png") {
			t.Errorf("FAIL upload-url: unexpected s3_key %q", key)
		}
		h.failsWith("confirm before PUT", h.call("confirm_property_image_upload", map[string]any{"s3_key": key}), "isn't in storage yet")

		putUpload(t, up, pngBytes)
		conf := h.call("confirm_property_image_upload", map[string]any{"s3_key": key, "type": "floor_plan"})
		if !h.ok("confirm", conf) {
			t.FailNow()
		}
		defer func() {
			h.ok("cleanup: remove test photo", h.call("remove_property_images", map[string]any{"keys": []any{key}}))
		}()
		if dig(conf.data, "image", "s3_key") != key || dig(conf.data, "image", "type") != "floor_plan" || dig(conf.data, "image", "url") == nil {
			t.Errorf("FAIL confirm: unexpected image %v", conf.data["image"])
		}
		again := h.call("confirm_property_image_upload", map[string]any{"s3_key": key, "type": "floor_plan"})
		if h.ok("confirm again (idempotent)", again) && again.data["imageCount"] != conf.data["imageCount"] {
			t.Errorf("FAIL confirm again: imageCount %v → %v (duplicate?)", conf.data["imageCount"], again.data["imageCount"])
		}

		// Non-image bytes behind a .png name are refused at confirm.
		bad := h.call("get_property_image_upload_url", map[string]any{"file_name": "fake.png"})
		if h.ok("upload-url (fake)", bad) {
			putUpload(t, bad, []byte("this is not an image"))
			h.failsWith("confirm non-image", h.call("confirm_property_image_upload", map[string]any{"s3_key": bad.data["s3_key"]}), "aren't a valid image")
		}
	})

	t.Run("renovation render", func(t *testing.T) {
		h.failsWith("upload-url: blank tag", h.call("get_renovation_image_upload_url", map[string]any{"file_name": "r.png", "tag_type": " "}), "tag_type is required")
		h.failsWith("upload-url: bad tag", h.call("get_renovation_image_upload_url", map[string]any{"file_name": "r.png", "tag_type": ":1"}), "canonical room tag")
		h.failsWith("confirm: foreign key", h.call("confirm_renovation_image_upload", map[string]any{"s3_key": "properties/" + foreign + "/generated/x/img-1.png"}), "THIS propertyID")

		up := h.call("get_renovation_image_upload_url", map[string]any{"file_name": "kitchen-render.png", "tag_type": "kitchen:1"})
		if !h.ok("upload-url", up) {
			t.FailNow()
		}
		key, _ := up.data["s3_key"].(string)
		if !strings.HasPrefix(key, "properties/"+h.property+"/generated/") {
			t.Errorf("FAIL upload-url: unexpected s3_key %q", key)
		}
		h.failsWith("confirm before PUT", h.call("confirm_renovation_image_upload", map[string]any{"s3_key": key}), "isn't in storage yet")
		putUpload(t, up, pngBytes)
		conf := h.call("confirm_renovation_image_upload", map[string]any{"s3_key": key})
		if h.ok("confirm", conf) && conf.data["s3_key"] != key {
			t.Errorf("FAIL confirm: s3_key %v, want %s", conf.data["s3_key"], key)
		}
		h.ok("confirm again (idempotent)", h.call("confirm_renovation_image_upload", map[string]any{"s3_key": key}))
		t.Logf("render %s left unsubmitted in storage (not tracked on the property)", key)
	})

	t.Run("document from URL", func(t *testing.T) {
		h.failsWith("not a URL", h.call("upload_property_document_from_url", map[string]any{"document_url": "/Users/me/a.pdf", "category": "additionalDocuments"}), "public http(s) link")
		h.failsWith("invented category", h.call("upload_property_document_from_url", map[string]any{"document_url": "https://example.com/a.pdf", "category": "misc"}), "unknown category")
		h.failsWith("unfetchable", h.call("upload_property_document_from_url", map[string]any{"document_url": "https://nonexistent.invalid/a.pdf", "category": "additionalDocuments"}), "couldn't download")
		h.failsWith("HTML page as pdf", h.call("upload_property_document_from_url", map[string]any{"document_url": "https://example.com/", "file_name": "page.pdf", "category": "additionalDocuments"}), "doesn't serve the file")
		h.failsWith("type not allowed for URL", h.call("upload_property_document_from_url", map[string]any{"document_url": "https://example.com/", "file_name": "clip.mp4", "category": "additionalDocuments"}), "URL uploads accept only")

		// Source: a fresh presigned view URL of a .txt document already on the property.
		list := h.call("list_property_documents", map[string]any{"category": "additionalDocuments"})
		if !h.ok("list (source)", list) {
			t.FailNow()
		}
		var source string
		docs, _ := list.data["documents"].([]any)
		for _, d := range docs {
			if n, _ := dig(d, "name").(string); strings.HasSuffix(n, ".txt") {
				source, _ = dig(d, "url").(string)
			}
		}
		if source == "" {
			t.Skip("no .txt document on the property to use as a URL source (run LiveDocuments first)")
		}
		name := fmt.Sprintf("mcp-url-test-%d.txt", time.Now().Unix())
		up := h.call("upload_property_document_from_url", map[string]any{"document_url": source, "file_name": name, "category": "additionalDocuments"})
		if !h.ok("upload from URL", up) {
			t.FailNow()
		}
		key, _ := up.data["s3_key"].(string)
		if up.data["name"] != name || up.data["category"] != "additionalDocuments" || !strings.HasPrefix(key, "PROPERTY_DOCUMENT/"+h.property+"/") {
			t.Errorf("FAIL upload from URL: unexpected document %v", up.data)
		}
		after := h.call("list_property_documents", map[string]any{"category": "additionalDocuments"})
		found := false
		docs, _ = after.data["documents"].([]any)
		for _, d := range docs {
			if dig(d, "s3_key") == key {
				found = true
			}
		}
		if h.ok("list (after)", after) && !found {
			t.Errorf("FAIL list: %s not listed", key)
		}
		t.Logf("left permanent test document %s (%s) on %s", name, key, h.property)
	})
}
