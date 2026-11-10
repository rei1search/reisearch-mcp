package tools

import (
	"strings"
	"testing"
)

func TestNormalizeCompType(t *testing.T) {
	ok := map[string]string{
		"sold":               "sold",
		" Sold ":             "sold",
		"long term rental":   "long term rental",
		"Long_Term_Rental":   "long term rental",
		"mid-term rental":    "mid term rental",
		"short  term rental": "short term rental",
		"affordable_housing": "affordable housing",
		"AFFORDABLE HOUSING": "affordable housing",
	}
	for in, want := range ok {
		if got, err := normalizeCompType(in); err != nil || got != want {
			t.Errorf("normalizeCompType(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"rental", "banana", "str", "longterm rental"} {
		if _, err := normalizeCompType(in); err == nil || !strings.Contains(err.Error(), "unknown compType") {
			t.Errorf("normalizeCompType(%q) err = %v, want unknown compType", in, err)
		}
	}
	if _, err := normalizeCompType("  "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("blank compType err = %v, want required", err)
	}
}
