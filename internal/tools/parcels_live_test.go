package tools

// Live test of the parcel tools against the REAL API (shared harness, see
// underwriting_live_test.go). Parcels are SHARED county data, so this only:
// looks one up, adds additional_details.mcp_test, checks a stale revision is
// refused, and removes the field again (the record ends unchanged; its
// revision rises by 2). create_parcel is exercised only on paths that write
// nothing, unless REISEARCH_LIVE_PARCEL_EXISTS=1 also tries re-creating the
// existing parcel (expects PARCEL_EXISTS).
//
// The account's plan must be enabled for parcels (otherwise every call is 429).
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<any property id, harness requirement> \
//	REISEARCH_LIVE_PARCELS=1 \
//	REISEARCH_LIVE_PARCEL_ADDRESS="120 W Comanche Ave, Tampa, FL 33604" \
//	REISEARCH_LIVE_PARCEL_FOLIO=1644130000 \
//	go test ./internal/tools/ -run LiveParcels -v

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestLiveParcels(t *testing.T) {
	if os.Getenv("REISEARCH_LIVE_PARCELS") != "1" {
		t.Skip("set REISEARCH_LIVE_PARCELS=1 to run (edits and restores a shared parcel record)")
	}
	address, folio := os.Getenv("REISEARCH_LIVE_PARCEL_ADDRESS"), os.Getenv("REISEARCH_LIVE_PARCEL_FOLIO")
	if address == "" || folio == "" {
		t.Fatal("set REISEARCH_LIVE_PARCEL_ADDRESS and REISEARCH_LIVE_PARCEL_FOLIO")
	}
	h := newLiveHarness(t)
	pk0 := "0123456789abcdef0123456789abcdef"

	// 1. Client-side validation (no API call).
	h.failsWith("get: blank address", h.call("get_parcel", map[string]any{"address": " "}), "address is required")
	h.failsWith("update: bad pk", h.call("update_parcel", map[string]any{"pk": "ABC", "folio": folio, "expected_revision": 0, "patch": map[string]any{"lat": 1}}), "32-character")
	h.failsWith("update: empty patch", h.call("update_parcel", map[string]any{"pk": pk0, "folio": folio, "expected_revision": 0, "patch": map[string]any{}}), "patch is required")
	h.failsWith("update: immutable", h.call("update_parcel", map[string]any{"pk": pk0, "folio": folio, "expected_revision": 0, "patch": map[string]any{"folio_number": "x"}}), "can never be changed")
	h.failsWith("update: read-only", h.call("update_parcel", map[string]any{"pk": pk0, "folio": folio, "expected_revision": 0, "patch": map[string]any{"revision": 9}}), "managed by the server")
	h.failsWith("create: read-only", h.call("create_parcel", map[string]any{"parcel": map[string]any{"PK": pk0}}), "set by the server")

	// 2. API: not found, and a create that fails validation (nothing written).
	h.failsWith("get: unknown address", h.call("get_parcel", map[string]any{"address": "1 Nonexistent Mcptest Rd, Nowhere, ZZ 00000"}), "PARCEL_NOT_FOUND")
	h.failsWith("create: missing fields", h.call("create_parcel", map[string]any{"parcel": map[string]any{"folio_number": "MCPTEST"}}), "VALIDATION")
	h.failsWith("update: unknown pk", h.call("update_parcel", map[string]any{"pk": pk0, "folio": folio, "expected_revision": 0, "patch": map[string]any{"additional_details": map[string]any{"mcp_test": "x"}}}), "PARCEL_NOT_FOUND")

	// 3. Lookup.
	got := h.call("get_parcel", map[string]any{"address": address, "folio": folio})
	if !h.ok("get_parcel", got) {
		t.FailNow()
	}
	pk, _ := got.data["pk"].(string)
	rev, _ := got.data["revision"].(float64)
	t.Logf("parcel pk=%s folio=%v revision=%v matched_by=%v", pk, got.data["folio"], rev, got.data["matched_by"])
	if got.data["folio"] != folio || got.data["record"] == nil {
		t.Fatalf("FAIL get_parcel: unexpected result %v", got.data)
	}

	// 4. Add a test field, refuse a stale revision, remove the field.
	marker := fmt.Sprintf("mcp live test %d — safe to ignore", time.Now().Unix())
	upd := h.call("update_parcel", map[string]any{"pk": pk, "folio": folio, "expected_revision": rev,
		"patch": map[string]any{"additional_details": map[string]any{"mcp_test": marker}}})
	if !h.ok("update: add test field", upd) {
		t.FailNow()
	}
	rev2, _ := upd.data["revision"].(float64)
	if rev2 != rev+1 || dig(upd.data, "record", "additional_details", "mcp_test") != marker {
		t.Errorf("FAIL update: revision %v → %v, record field %v", rev, rev2, dig(upd.data, "record", "additional_details", "mcp_test"))
	}
	// Cleanup: remove only the test key, or the whole additional_details object
	// if the record didn't have one before (otherwise an empty {} is left).
	rec0, _ := got.data["record"].(map[string]any)
	_, hadDetails := rec0["additional_details"]
	defer func() {
		var undo map[string]any
		if hadDetails {
			undo = map[string]any{"additional_details": map[string]any{"mcp_test": nil}}
		} else {
			undo = map[string]any{"additional_details": nil}
		}
		r := h.call("update_parcel", map[string]any{"pk": pk, "folio": folio, "expected_revision": rev2, "patch": undo})
		if h.ok("cleanup: remove test field", r) && dig(r.data, "record", "additional_details", "mcp_test") != nil {
			t.Errorf("FAIL cleanup: mcp_test still present")
		}
		if _, still := dig(r.data, "record").(map[string]any)["additional_details"]; !hadDetails && still {
			t.Errorf("FAIL cleanup: additional_details left behind")
		}
	}()
	h.failsWith("update: stale revision", h.call("update_parcel", map[string]any{"pk": pk, "folio": folio, "expected_revision": rev,
		"patch": map[string]any{"additional_details": map[string]any{"mcp_test": "stale"}}}), "someone else changed")
	h.failsWith("update: bad type", h.call("update_parcel", map[string]any{"pk": pk, "folio": folio, "expected_revision": rev2,
		"patch": map[string]any{"valuation": map[string]any{"market_value": "100"}}}), "VALIDATION")

	// 5. Optional: re-create the existing parcel → PARCEL_EXISTS, nothing written.
	if os.Getenv("REISEARCH_LIVE_PARCEL_EXISTS") == "1" {
		rec, _ := got.data["record"].(map[string]any)
		h.failsWith("create: existing", h.call("create_parcel", map[string]any{"parcel": map[string]any{
			"folio_number": folio, "property_address": rec["property_address"], "source": rec["source"],
		}}), "PARCEL_EXISTS")
	}
}
