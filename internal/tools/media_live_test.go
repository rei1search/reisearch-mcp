package tools

// Live end-to-end test of the property media tools against the REAL public
// API. Same env vars and harness as TestLiveUnderwriting (skipped unless set):
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<throwaway property id> \
//	go test ./internal/tools/ -run LiveMedia -v
//
// Optional REISEARCH_LIVE_IMAGE_URL (a public image; defaults to an Unsplash
// photo). The uploaded test photo is removed again at the end, even when a
// step fails, so the property's gallery ends where it started.

import (
	"os"
	"slices"
	"testing"
)

func TestLiveMedia(t *testing.T) {
	h := newLiveHarness(t)
	img := os.Getenv("REISEARCH_LIVE_IMAGE_URL")
	if img == "" {
		img = "https://images.unsplash.com/photo-1556909114-f6e7ad7d3136?w=1200"
	}

	keysOf := func(r liveResult) []string {
		var keys []string
		images, _ := r.data["images"].([]any)
		for _, im := range images {
			if k, ok := dig(im, "s3_key").(string); ok {
				keys = append(keys, k)
			}
		}
		return keys
	}

	// 1. Baseline. Images must not carry renovation tags any more.
	before := h.call("list_property_images", map[string]any{})
	if !h.ok("list images (baseline)", before) {
		t.FailNow()
	}
	baseline := keysOf(before)

	// 2. Client-side validation (no API call).
	h.failsWith("upload: not a URL", h.call("upload_property_image", map[string]any{"image_url": "/Users/me/front.jpg"}), "http(s)")
	h.failsWith("upload: bad type", h.call("upload_property_image", map[string]any{"image_url": img, "type": "panorama"}), "floor_plan")
	h.failsWith("remove: no keys", h.call("remove_property_images", map[string]any{"keys": []any{" "}}), "at least one")

	// 3. API errors get hints.
	h.failsWith("upload: unfetchable URL", h.call("upload_property_image", map[string]any{
		"image_url": "https://images.unsplash.com/this-photo-does-not-exist-mcp-test.jpg",
	}), "public, direct link")

	// 4. Upload as a floor plan; remove it again at the end no matter what.
	up := h.call("upload_property_image", map[string]any{"image_url": img, "type": "floor_plan"})
	if !h.ok("upload", up) {
		t.FailNow()
	}
	key, _ := dig(up.data, "image", "s3_key").(string)
	removed := false
	t.Cleanup(func() {
		if key != "" && !removed {
			t.Logf("cleanup: removing test image %s", key)
			h.call("remove_property_images", map[string]any{"keys": []any{key}})
		}
	})
	if dig(up.data, "image", "type") != "floor_plan" {
		t.Errorf("FAIL upload: type = %v, want floor_plan", dig(up.data, "image", "type"))
	}
	if dig(up.data, "image", "url") == nil {
		t.Errorf("FAIL upload: no view url on the new image")
	}
	if got, _ := up.data["imageCount"].(float64); int(got) != len(baseline)+1 {
		t.Errorf("FAIL upload: imageCount = %v, want %d", got, len(baseline)+1)
	}

	// 5. The new image shows up in the list.
	after := h.call("list_property_images", map[string]any{})
	if h.ok("list images (after upload)", after) && !slices.Contains(keysOf(after), key) {
		t.Errorf("FAIL list: uploaded key %s not listed", key)
	}

	// 6. All-or-nothing: one bad key → nothing removed, with the hint.
	h.failsWith("remove: unknown key", h.call("remove_property_images", map[string]any{
		"keys": []any{key, "PROPERTY_IMAGES/does-not-exist.jpg"},
	}), "NOTHING was removed")
	still := h.call("list_property_images", map[string]any{})
	if h.ok("list images (after failed remove)", still) && !slices.Contains(keysOf(still), key) {
		t.Errorf("FAIL all-or-nothing: %s disappeared after a failed remove", key)
	}

	// 7. Remove it (duplicate key in the input is de-duplicated).
	rm := h.call("remove_property_images", map[string]any{"keys": []any{key, " " + key + " "}})
	if h.ok("remove", rm) {
		removed = true
		if remaining, _ := rm.data["remainingKeys"].([]any); slices.Contains(remaining, any(key)) {
			t.Errorf("FAIL remove: %s still in remainingKeys", key)
		}
	}

	// 8. Gallery is back to the baseline.
	end := h.call("list_property_images", map[string]any{})
	if h.ok("list images (end)", end) && !slices.Equal(keysOf(end), baseline) {
		t.Errorf("FAIL cleanup: gallery %v differs from baseline %v", keysOf(end), baseline)
	}
}
