package tools

// Live end-to-end test of the property document tools against the REAL public
// API, using the shared harness (see underwriting_live_test.go).
//
// The API has NO document delete endpoint, so this test leaves one small .txt
// document on the property for good. It therefore needs an extra opt-in:
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<throwaway property id> \
//	REISEARCH_LIVE_DOCS=1 \
//	go test ./internal/tools/ -run LiveDocuments -v

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveDocuments(t *testing.T) {
	if os.Getenv("REISEARCH_LIVE_DOCS") != "1" {
		t.Skip("set REISEARCH_LIVE_DOCS=1 to run (adds a permanent test document)")
	}
	h := newLiveHarness(t)

	docsOf := func(r liveResult) []any {
		docs, _ := r.data["documents"].([]any)
		return docs
	}
	hasKey := func(r liveResult, key string) bool {
		for _, d := range docsOf(r) {
			if dig(d, "s3_key") == key {
				return true
			}
		}
		return false
	}

	// 1. Baseline.
	before := h.call("list_property_documents", map[string]any{})
	if !h.ok("list documents (baseline)", before) {
		t.FailNow()
	}
	baseline := len(docsOf(before))
	if got, _ := before.data["count"].(float64); int(got) != baseline {
		t.Errorf("FAIL list: count %v != len(documents) %d", got, baseline)
	}
	h.ok("list documents (category filter)", h.call("list_property_documents", map[string]any{"category": "inspectionReport"}))

	// 2. Client-side validation (no API call).
	h.failsWith("list: invented category", h.call("list_property_documents", map[string]any{"category": "misc"}), "additionalDocuments")
	h.failsWith("upload-url: no file_name", h.call("get_property_document_upload_url", map[string]any{"file_name": " "}), "file_name is required")
	h.failsWith("confirm: no category", h.call("confirm_property_document_upload", map[string]any{"s3_key": "x", "file_name": "a.txt"}), "category")
	h.failsWith("confirm: plural category", h.call("confirm_property_document_upload", map[string]any{"s3_key": "x", "file_name": "a.txt", "category": "helocStatements"}), "unknown category")

	// 3. API rejections with hints.
	h.failsWith("upload-url: bad file type", h.call("get_property_document_upload_url", map[string]any{"file_name": "setup.exe"}), "isn't accepted")
	h.failsWith("confirm: foreign key", h.call("confirm_property_document_upload", map[string]any{
		"s3_key": "PROPERTY_DOCUMENT/00000000-0000-0000-0000-000000000000/11111111-1111-1111-1111-111111111111.txt", "file_name": "a.txt", "category": "additionalDocuments",
	}), "THIS propertyID")

	// 4. Step 1: upload URL.
	name := fmt.Sprintf("mcp-test-%d.txt", time.Now().Unix())
	up := h.call("get_property_document_upload_url", map[string]any{"file_name": name})
	if !h.ok("upload-url", up) {
		t.FailNow()
	}
	key, _ := up.data["s3_key"].(string)
	uploadURL, _ := up.data["upload_url"].(string)
	contentType, _ := dig(up.data, "headers", "Content-Type").(string)
	if !strings.HasPrefix(key, "PROPERTY_DOCUMENT/"+h.property+"/") || !strings.HasSuffix(key, ".txt") {
		t.Errorf("FAIL upload-url: unexpected s3_key %q", key)
	}
	if contentType != "text/plain" {
		t.Errorf("FAIL upload-url: Content-Type header %q, want text/plain", contentType)
	}

	// 5. Confirm before the PUT → not uploaded yet.
	confirmArgs := func(category string) map[string]any {
		return map[string]any{"s3_key": key, "file_name": name, "category": category}
	}
	h.failsWith("confirm before PUT", h.call("confirm_property_document_upload", confirmArgs("additionalDocuments")), "isn't in storage yet")

	// 6. Step 2: the PUT the AI client does itself.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader([]byte("ReiSearch MCP live test document. Safe to ignore.\n")))
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("FAIL PUT to upload_url: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FAIL PUT to upload_url: status %d", resp.StatusCode)
	}

	// 7. Step 3: confirm.
	conf := h.call("confirm_property_document_upload", confirmArgs("additionalDocuments"))
	if !h.ok("confirm", conf) {
		t.FailNow()
	}
	if conf.data["s3_key"] != key || conf.data["name"] != name || conf.data["category"] != "additionalDocuments" || conf.data["url"] == nil {
		t.Errorf("FAIL confirm: unexpected document %v", conf.data)
	}

	// 8. Retry is idempotent; another category is a 409.
	h.ok("confirm again (idempotent)", h.call("confirm_property_document_upload", confirmArgs("additionalDocuments")))
	h.failsWith("confirm other category", h.call("confirm_property_document_upload", confirmArgs("inspectionReport")), "already recorded")

	// 9. Listed exactly once.
	after := h.call("list_property_documents", map[string]any{})
	if h.ok("list documents (after)", after) {
		if !hasKey(after, key) {
			t.Errorf("FAIL list: %s not listed", key)
		}
		if n := len(docsOf(after)); n != baseline+1 {
			t.Errorf("FAIL list: %d documents, want %d (duplicate on retry?)", n, baseline+1)
		}
	}
	filtered := h.call("list_property_documents", map[string]any{"category": "additionalDocuments"})
	if h.ok("list documents (additionalDocuments)", filtered) && !hasKey(filtered, key) {
		t.Errorf("FAIL list filter: %s not in additionalDocuments", key)
	}
	t.Logf("left permanent test document %s (%s) on %s", name, key, h.property)
}
