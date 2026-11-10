package tools

// Live read-only test of get_comps against the REAL API (shared harness, see
// underwriting_live_test.go). Never runs comps (billed).
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<property id with comps> \
//	REISEARCH_LIVE_COMPS=1 \
//	go test ./internal/tools/ -run LiveComps -v

import (
	"os"
	"testing"
)

func TestLiveComps(t *testing.T) {
	if os.Getenv("REISEARCH_LIVE_COMPS") != "1" {
		t.Skip("set REISEARCH_LIVE_COMPS=1 to run")
	}
	h := newLiveHarness(t)

	for _, ct := range compTypes {
		r := h.call("get_comps", map[string]any{"compType": ct})
		if h.ok("get_comps "+ct, r) {
			comps, _ := r.data["comps"].([]any)
			t.Logf("%-18s status=%v comps=%d", ct, r.data["status"], len(comps))
			if r.data["status"] == nil || r.data["message"] == nil {
				t.Errorf("FAIL %s: missing status or message", ct)
			}
		}
	}
	r := h.call("get_comps", map[string]any{"compType": "Short_Term_Rental"})
	if h.ok("get_comps normalized", r) && r.data["status"] == nil {
		t.Errorf("FAIL normalized: no status")
	}
	h.failsWith("plain rental", h.call("get_comps", map[string]any{"compType": "rental"}), "unknown compType")
	h.failsWith("made up", h.call("get_comps", map[string]any{"compType": "banana"}), "unknown compType")
	h.failsWith("blank", h.call("get_comps", map[string]any{"compType": " "}), "compType is required")
}
