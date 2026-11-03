package tools

// Live end-to-end test of the underwriting v6 tools (+ the renovation submit
// fix) against the REAL public API. It is skipped unless both env vars are set,
// so a plain `go test` never touches production:
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<throwaway property id> \
//	go test ./internal/tools/ -run Live -v
//
// Optional: REISEARCH_LIVE_API (default https://api-pub.reisearch.com) and
// REISEARCH_LIVE_IMAGE_URL (a public image; enables the full renovation
// submit flow — it leaves one render on the property's ledger).
//
// Tools are called over the real MCP protocol (streamable HTTP) against an
// in-process server; the token is injected exactly where the bearer
// middleware puts it. Every version the test creates is deleted at the end,
// even when a step fails.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

type liveResult struct {
	isError bool
	text    string
	data    map[string]any
}

type liveHarness struct {
	t        *testing.T
	session  *mcp.ClientSession
	property string
	strategy string
}

func (h *liveHarness) call(name string, args map[string]any) liveResult {
	h.t.Helper()
	if _, ok := args["propertyID"]; !ok {
		args["propertyID"] = h.property
	}
	// Image/ledger tools take no strategy; the SDK rejects unknown arguments.
	noStrategy := name == "save_renovation_image" || name == "get_renovation_ledger"
	if _, ok := args["strategy"]; !ok && !noStrategy {
		args["strategy"] = h.strategy
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := h.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: protocol error: %v", name, err)
	}
	out := liveResult{isError: res.IsError}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out.text += tc.Text
		}
	}
	if !out.isError {
		_ = json.Unmarshal([]byte(out.text), &out.data)
	}
	shown := out.text
	if len(shown) > 600 {
		shown = shown[:600] + "…"
	}
	h.t.Logf("%s isError=%v → %s", name, out.isError, shown)
	return out
}

// ok fails the step when the tool errored.
func (h *liveHarness) ok(step string, r liveResult) bool {
	h.t.Helper()
	if r.isError {
		h.t.Errorf("FAIL %s: unexpected error: %s", step, r.text)
		return false
	}
	return true
}

// failsWith fails the step unless the tool errored with text containing want.
func (h *liveHarness) failsWith(step string, r liveResult, want string) {
	h.t.Helper()
	if !r.isError || !strings.Contains(r.text, want) {
		h.t.Errorf("FAIL %s: want an error containing %q, got isError=%v %s", step, want, r.isError, r.text)
	}
}

// dig walks nested maps: dig(m, "a", "b") == m["a"]["b"].
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func TestLiveUnderwriting(t *testing.T) {
	token := os.Getenv("REISEARCH_LIVE_TOKEN")
	property := os.Getenv("REISEARCH_LIVE_PROPERTY")
	if token == "" || property == "" {
		t.Skip("set REISEARCH_LIVE_TOKEN and REISEARCH_LIVE_PROPERTY to run the live underwriting test")
	}
	api := os.Getenv("REISEARCH_LIVE_API")
	if api == "" {
		api = "https://api-pub.reisearch.com"
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "reisearch-live", Version: "v0"}, nil)
	Register(server, reisearch.NewClient(api))
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpHandler.ServeHTTP(w, r.WithContext(WithToken(r.Context(), token)))
	}))
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "live-client", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	h := &liveHarness{t: t, session: session, property: property, strategy: "fixFlip"}

	// Delete every version we create, even if a later step fails.
	created := map[string]bool{}
	t.Cleanup(func() {
		for id, alive := range created {
			if alive {
				t.Logf("cleanup: deleting leftover version %s", id)
				h.call("delete_underwriting_version", map[string]any{"versionId": id})
			}
		}
	})

	// 1. Baseline + client-side strategy check.
	base := h.call("list_underwriting_versions", map[string]any{})
	if !h.ok("list versions (baseline)", base) {
		t.FailNow() // token/property problem — nothing else will work
	}
	baseline, _ := base.data["count"].(float64)
	if _, isList := base.data["versions"].([]any); !isList {
		t.Errorf("FAIL list versions: 'versions' should always be a list, got %v", base.data["versions"])
	}
	h.failsWith("missing strategy", h.call("list_underwriting_versions", map[string]any{"strategy": ""}), "strategy")

	// 2. Create a section in a new version.
	cr := h.call("create_underwriting_section", map[string]any{
		"section": "appliances", "create_new": true, "version_name": "mcp-test",
		"data": map[string]any{"stove": map[string]any{"materialCost": 100, "laborCost": 50, "total": 150}},
	})
	if !h.ok("create section (create_new)", cr) {
		t.FailNow()
	}
	v, _ := cr.data["versionId"].(string)
	if v == "" {
		t.Fatalf("FAIL create section: no versionId in %s", cr.text)
	}
	created[v] = true

	// 3. Same create again → 409 with the update hint.
	h.failsWith("create existing section → 409", h.call("create_underwriting_section", map[string]any{
		"section": "appliances", "versionId": v,
		"data": map[string]any{"stove": map[string]any{"total": 1}},
	}), "update_underwriting_section")

	// 4. Object merge: only laborCost changes; materialCost survives.
	h.ok("update object (merge)", h.call("update_underwriting_section", map[string]any{
		"section": "appliances", "path": "stove", "versionId": v, "value": map[string]any{"laborCost": 75},
	}))
	app := h.call("get_underwriting_section", map[string]any{"section": "appliances", "versionId": v})
	if h.ok("get appliances", app) {
		if got := dig(app.data, "data", "stove", "laborCost"); got != float64(75) {
			t.Errorf("FAIL merge: stove.laborCost = %v, want 75", got)
		}
		if got := dig(app.data, "data", "stove", "materialCost"); got != float64(100) {
			t.Errorf("FAIL merge: stove.materialCost = %v, want 100 (sibling should be kept)", got)
		}
	}

	// 5. Scalar replace (also creates repairRenovation in this version).
	h.ok("update scalar", h.call("update_underwriting_section", map[string]any{
		"section": "repairRenovation", "path": "estimatedTotal", "versionId": v, "value": 2450,
	}))

	// 6. Unknown path rejected client-side; unwritten section → found:false.
	h.failsWith("unknown path", h.call("update_underwriting_section", map[string]any{
		"section": "repairRenovation", "path": "bogusPath", "versionId": v, "value": 1,
	}), "allowed paths")
	empty := h.call("get_underwriting_section", map[string]any{"section": "saleExit", "versionId": v})
	if h.ok("get unwritten section", empty) && empty.data["found"] != false {
		t.Errorf("FAIL unwritten section: want found:false, got %s", empty.text)
	}

	// 7. Room upsert, then a cost-only upsert must keep lineItems.
	h.ok("upsert room", h.call("upsert_underwriting_room", map[string]any{
		"area": "kitchen", "tag": "kitchen:1", "versionId": v,
		"room": map[string]any{
			"name": "Kitchen", "source": "manual",
			"cost":      map[string]any{"materialCost": 800, "laborCost": 400, "total": 1200},
			"lineItems": []any{map[string]any{"name": "Cabinets", "cost": map[string]any{"total": 1200}}},
		},
	}))
	h.ok("upsert room cost-only", h.call("upsert_underwriting_room", map[string]any{
		"area": "kitchen", "tag": "kitchen:1", "versionId": v,
		"room": map[string]any{"cost": map[string]any{"laborCost": 500, "total": 1300}},
	}))
	rr := h.call("get_underwriting_section", map[string]any{"section": "repairRenovation", "versionId": v})
	if h.ok("get repairRenovation", rr) {
		room := dig(rr.data, "data", "interiorRenovations", "kitchen", "items", "kitchen:1")
		if items, _ := dig(room, "lineItems").([]any); len(items) != 1 {
			t.Errorf("FAIL room merge: lineItems should survive a cost-only upsert, room = %v", room)
		}
		if got := dig(room, "cost", "laborCost"); got != float64(500) {
			t.Errorf("FAIL room merge: cost.laborCost = %v, want 500", got)
		}
		if got := dig(rr.data, "data", "estimatedTotal"); got != float64(2450) {
			t.Errorf("FAIL scalar: estimatedTotal = %v, want 2450", got)
		}
	}

	// 8. Clone, rename (incl. same-name skip), deal score.
	cl := h.call("clone_underwriting_version", map[string]any{"versionId": v, "version_name": "mcp-test-clone"})
	clone := ""
	if h.ok("clone", cl) {
		clone, _ = cl.data["versionId"].(string)
		created[clone] = true
		if cl.data["sourceVersionId"] != v {
			t.Errorf("FAIL clone: sourceVersionId = %v, want %s", cl.data["sourceVersionId"], v)
		}
	}
	if clone != "" {
		h.ok("rename", h.call("rename_underwriting_version", map[string]any{"versionId": clone, "version_name": "mcp-test-clone-2"}))
		same := h.call("rename_underwriting_version", map[string]any{"versionId": clone, "version_name": "mcp-test-clone-2"})
		if h.ok("rename to same name", same) && same.data["skipped"] != true {
			t.Errorf("FAIL rename same name: want skipped:true, got %s", same.text)
		}
	}
	h.ok("deal score 82.5", h.call("set_underwriting_deal_score", map[string]any{"versionId": v, "deal_score": 82.5}))
	h.failsWith("deal score 101", h.call("set_underwriting_deal_score", map[string]any{"versionId": v, "deal_score": 101}), "between 0 and 100")

	// 9. get_all: our version present, bookkeeping keys stripped.
	all := h.call("get_all_underwriting", map[string]any{})
	if h.ok("get all", all) {
		found := false
		entries, _ := all.data["entries"].([]any)
		for _, e := range entries {
			if dig(e, "versionId") != v {
				continue
			}
			found = true
			sections, _ := dig(e, "underwriting").(map[string]any)
			for name, sec := range sections {
				for _, k := range []string{"propertyId", "createdById", "createdAt", "updatedAt"} {
					if dig(sec, k) != nil {
						t.Errorf("FAIL get all: section %s still has bookkeeping key %s", name, k)
					}
				}
			}
		}
		if !found {
			t.Errorf("FAIL get all: test version %s not in entries", v)
		}
	}

	// 10. Strategy isolation: the fixFlip version is invisible under coLiving.
	h.failsWith("strategy isolation", h.call("get_underwriting_section", map[string]any{
		"strategy": "coLiving", "section": "appliances", "versionId": v,
	}), "strategy mismatch")

	// 11. Renovation submit: strategy required; full flow if an image URL is given.
	estimate := map[string]any{"lineItems": []any{map[string]any{"name": "Cabinets", "materialCost": 100, "laborCost": 50, "total": 150}}}
	h.failsWith("submit without strategy", h.call("submit_property_renovation", map[string]any{
		"strategy": "", "s3_key": "x", "tag_type": "kitchen:1", "estimate": estimate, "version_id": v,
	}), "strategy")
	if img := os.Getenv("REISEARCH_LIVE_IMAGE_URL"); img != "" {
		saved := h.call("save_renovation_image", map[string]any{"image_url": img, "tag_type": "kitchen:2"})
		if h.ok("save render", saved) {
			key, _ := saved.data["s3_key"].(string)
			sub := h.call("submit_property_renovation", map[string]any{
				"s3_key": key, "tag_type": "kitchen:2", "estimate": estimate, "version_id": v,
			})
			if h.ok("submit renovation", sub) {
				if sub.data["versionId"] != v || sub.data["room"] == nil {
					t.Errorf("FAIL submit: want { versionId: %s, room }, got %s", v, sub.text)
				}
			}
			led := h.call("get_renovation_ledger", map[string]any{"sections": []any{"after", "accepted"}})
			if h.ok("ledger after submit", led) && !strings.Contains(led.text, key) {
				t.Errorf("FAIL ledger: submitted render %s not on the ledger", key)
			}
		}
	} else {
		t.Log("skip: full renovation submit (set REISEARCH_LIVE_IMAGE_URL to run it)")
	}

	// 12. Delete room (twice → room not found), then both versions.
	h.ok("delete room", h.call("delete_underwriting_room", map[string]any{"area": "kitchen", "tag": "kitchen:1", "versionId": v}))
	h.failsWith("delete missing room", h.call("delete_underwriting_room", map[string]any{"area": "kitchen", "tag": "kitchen:1", "versionId": v}), "room not found")
	for _, id := range []string{clone, v} {
		if id != "" && h.ok("delete version", h.call("delete_underwriting_version", map[string]any{"versionId": id})) {
			created[id] = false
		}
	}
	end := h.call("list_underwriting_versions", map[string]any{})
	if h.ok("list versions (end)", end) && end.data["count"] != baseline {
		t.Errorf("FAIL cleanup: %v versions at the end, want baseline %v", end.data["count"], baseline)
	}
}
