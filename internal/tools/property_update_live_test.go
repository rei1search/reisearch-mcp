package tools

// Live end-to-end test of update_property against the REAL public API, using
// the shared harness (see underwriting_live_test.go):
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<throwaway property id> \
//	go test ./internal/tools/ -run LivePropertyUpdate -v
//
// It changes propertyDetails.hoaDues (+1) and appends to
// additionalInfo.others.indoorFeatures, then restores both, even when a step
// fails. The property's values end where they started; its updatedAt moves.

import (
	"slices"
	"testing"
)

func TestLivePropertyUpdate(t *testing.T) {
	h := newLiveHarness(t)

	details := func(step string) (version int64, hoa float64, features []any) {
		r := h.call("get_property_details", map[string]any{})
		if !h.ok(step, r) {
			t.FailNow()
		}
		v, _ := dig(r.data, "property_detail", "updatedAt").(float64)
		hoa, _ = dig(r.data, "property_detail", "propertyDetails", "hoaDues").(float64)
		features, _ = dig(r.data, "property_detail", "additionalInfo", "others", "indoorFeatures").([]any)
		return int64(v), hoa, features
	}
	changed := func(r liveResult) []any { c, _ := r.data["changedFields"].([]any); return c }

	// 1. Baseline.
	v0, hoa0, features0 := details("details (baseline)")
	if len(features0) == 0 {
		features0 = []any{"Fireplace"} // the API can't store an empty list; seed one
	}
	restore := map[string]any{
		"propertyDetails": map[string]any{"hoaDues": hoa0},
		"additionalInfo":  map[string]any{"others": map[string]any{"indoorFeatures": features0}},
	}
	t.Cleanup(func() { h.call("update_property", restore) })

	// 2. Client-side validation (no API call).
	h.failsWith("nothing to update", h.call("update_property", map[string]any{"propertyDetails": map[string]any{}}), "nothing to update")
	h.failsWith("bad dealType", h.call("update_property", map[string]any{"dealType": "loan"}), "'cash' or 'creative'")
	h.failsWith("cash + creativeStructure", h.call("update_property", map[string]any{"dealType": "cash", "creativeStructure": "stack"}), "clears it automatically")
	h.failsWith("address not accepted", h.call("update_property", map[string]any{"location": "1 Main St"}), "location")

	// 3. API validation: all or nothing, every field listed.
	h.failsWith("bad bedrooms+year", h.call("update_property", map[string]any{"bedrooms": "three", "yearBuilt": "98"}), "yearBuilt")

	// 4. Dry run: reports the change, saves nothing.
	dry := h.call("update_property", map[string]any{"expectedVersion": v0, "dryRun": true, "propertyDetails": map[string]any{"hoaDues": hoa0 + 1}})
	if h.ok("dry run", dry) && (dry.data["dryRun"] != true || !slices.Contains(changed(dry), any("propertyDetails.hoaDues"))) {
		t.Errorf("FAIL dry run: %v", dry.data)
	}
	if v, hoa, _ := details("details (after dry run)"); hoa != hoa0 || v != v0 {
		t.Errorf("FAIL dry run saved: hoaDues %v (want %v), version %d (want %d)", hoa, hoa0, v, v0)
	}

	// 5. Real update: nested merge + list replace, with the version check.
	newFeatures := append(slices.Clone(features0), "MCP test feature")
	up := h.call("update_property", map[string]any{
		"expectedVersion": v0,
		"propertyDetails": map[string]any{"hoaDues": hoa0 + 1},
		"additionalInfo":  map[string]any{"others": map[string]any{"indoorFeatures": newFeatures}},
	})
	if !h.ok("update", up) {
		t.FailNow()
	}
	v1, _ := up.data["version"].(float64)
	if int64(v1) == v0 || len(changed(up)) != 2 {
		t.Errorf("FAIL update: version %v (old %d), changedFields %v", v1, v0, changed(up))
	}
	if dig(up.data, "property", "propertyDetails", "apn") == nil {
		t.Errorf("FAIL merge: propertyDetails.apn was dropped")
	}
	if _, hoa, features := details("details (after update)"); hoa != hoa0+1 || len(features) != len(newFeatures) {
		t.Errorf("FAIL update not stored: hoaDues %v, features %v", hoa, features)
	}

	// 6. Repeat with the new version: no change, version stays.
	again := h.call("update_property", map[string]any{"expectedVersion": int64(v1), "propertyDetails": map[string]any{"hoaDues": hoa0 + 1}})
	if h.ok("repeat (idempotent)", again) && (len(changed(again)) != 0 || again.data["version"] != v1) {
		t.Errorf("FAIL repeat: %v", again.data)
	}

	// 7. Stale version → 412 with the hint.
	h.failsWith("stale version", h.call("update_property", map[string]any{"expectedVersion": v0, "propertyDetails": map[string]any{"hoaDues": hoa0 + 2}}), "changed since you read it")

	// 8. Restore, then confirm.
	if h.ok("restore", h.call("update_property", restore)) {
		if _, hoa, features := details("details (end)"); hoa != hoa0 || !slices.Equal(features, features0) {
			t.Errorf("FAIL restore: hoaDues %v (want %v), features %v (want %v)", hoa, hoa0, features, features0)
		}
	}
}
