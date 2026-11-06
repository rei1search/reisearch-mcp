package tools

// Live test of invite_users against the REAL public API (shared harness, see
// underwriting_live_test.go). It can send real email, so it needs an extra
// opt-in and only ever invites addresses you pass in:
//
//	REISEARCH_LIVE_TOKEN=<bearer token> \
//	REISEARCH_LIVE_PROPERTY=<any property id, harness requirement> \
//	REISEARCH_LIVE_INVITES=1 \
//	REISEARCH_LIVE_INVITE_EXISTING=<email of an existing ReiSearch user> \  # queued, then skipped: no email
//	REISEARCH_LIVE_INVITE_EMAIL=<fresh address you control>               # optional: a real invite is emailed
//	go test ./internal/tools/ -run LiveInvitations -v

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLiveInvitations(t *testing.T) {
	if os.Getenv("REISEARCH_LIVE_INVITES") != "1" {
		t.Skip("set REISEARCH_LIVE_INVITES=1 to run (may send real invitation email)")
	}
	h := newLiveHarness(t)
	person := func(first string, emails ...string) map[string]any {
		list := make([]any, len(emails))
		for i, e := range emails {
			list[i] = e
		}
		return map[string]any{"invited_first_name": first, "invited_email": list}
	}
	invite := func(people ...map[string]any) liveResult {
		list := make([]any, len(people))
		for i, p := range people {
			list[i] = p
		}
		return h.call("invite_users", map[string]any{"invitations": list})
	}

	// 1. Client-side validation (no API call, nothing sent).
	h.failsWith("no people", invite(), "at least one person")
	h.failsWith("no first name", invite(person(" ", "a@example.com")), "invited_first_name is required")
	h.failsWith("blank email", invite(person("Ada", "a@example.com", " ")), "is blank")
	h.failsWith("display-name email", invite(person("Ada", "Ada <a@example.com>")), "not a bare address")
	many := make([]string, 101)
	for i := range many {
		many[i] = "dup@example.com"
	}
	h.failsWith("101 emails (dupes count)", invite(person("Ada", many...)), "too many emails")

	// 2. API: every email invalid → 400, nothing queued.
	h.failsWith("all invalid", invite(person("Nope", "not-an-email", "x@")), "No valid emails")

	// 3. Existing user + one invalid: queued (then skipped by the worker, so
	// no email), invalid one reported back.
	existing := os.Getenv("REISEARCH_LIVE_INVITE_EXISTING")
	if existing == "" {
		t.Log("REISEARCH_LIVE_INVITE_EXISTING not set: skipping the queued-path checks")
		return
	}
	mixed := invite(person("Existing", strings.ToUpper(existing), "not-an-email"))
	if h.ok("existing user + invalid", mixed) {
		if q, _ := mixed.data["queued"].(float64); q != 1 {
			t.Errorf("FAIL mixed: queued = %v, want 1", q)
		}
		if inv, _ := mixed.data["invalidEmails"].([]any); !slices.Contains(inv, any("not-an-email")) {
			t.Errorf("FAIL mixed: invalidEmails = %v", inv)
		}
		if mixed.data["note"] == nil {
			t.Errorf("FAIL mixed: no note about skipped emails")
		}
	}

	// 4. Optional real invite to a fresh address.
	if fresh := os.Getenv("REISEARCH_LIVE_INVITE_EMAIL"); fresh != "" {
		real := invite(map[string]any{"invited_first_name": "MCP", "invited_last_name": "Test", "invited_email": []any{fresh}})
		if h.ok("real invite", real) {
			t.Logf("invitation queued for %s — check that inbox for the sign-up email", fresh)
		}
	}
}
