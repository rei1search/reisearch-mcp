package tools

import "testing"

func TestPropertyAddress(t *testing.T) {
	full := map[string]any{"property_detail": map[string]any{"street": "9686 Fontainebleau Boulevard", "city": "Miami", "state": "FL", "zipCode": "33172", "location": "ignored"}}
	if got, want := propertyAddress(full), "9686 Fontainebleau Boulevard, Miami, FL 33172"; got != want {
		t.Errorf("propertyAddress = %q, want %q", got, want)
	}
	loc := map[string]any{"property_detail": map[string]any{"location": "1 Main St, Tampa, FL 33604"}}
	if got := propertyAddress(loc); got != "1 Main St, Tampa, FL 33604" {
		t.Errorf("fallback = %q", got)
	}
	if got := propertyAddress(map[string]any{}); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestWithUnit(t *testing.T) {
	cases := map[[2]string]string{
		{"9686 Fontainebleau Blvd, Miami, FL 33172", "507"}:  "9686 Fontainebleau Blvd 507, Miami, FL 33172",
		{"9686 Fontainebleau Blvd, Miami, FL 33172", "#507"}: "9686 Fontainebleau Blvd 507, Miami, FL 33172",
		{"9686 Fontainebleau Blvd", "507"}:                   "9686 Fontainebleau Blvd 507",
		{"1 Main St, Tampa, FL", " "}:                        "1 Main St, Tampa, FL",
	}
	for in, want := range cases {
		if got := withUnit(in[0], in[1]); got != want {
			t.Errorf("withUnit(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
