package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNDJSONTestRecord_JSONTags(t *testing.T) {
	rec := ndjsonTestRecord{
		Type:        "test",
		N:           1,
		Description: "foo.bar",
		OK:          true,
		Diagnostic:  map[string]any{"tool": "foo.bar"},
		Subtest:     []ndjsonTestRecord{},
		Line:        1,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"type":"test"`,
		`"n":1`,
		`"description":"foo.bar"`,
		`"ok":true`,
		`"directive":null`,
		`"output":null`,
		`"subtest":[]`,
		`"line":1`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestNDJSONSummaryRecord_JSONTags(t *testing.T) {
	rec := ndjsonSummaryRecord{
		Type:        "summary",
		Passed:      2,
		Total:       2,
		PlanCount:   2,
		Valid:       true,
		Diagnostics: []ndjsonSummaryDiagnostic{},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"type":"summary"`,
		`"passed":2`,
		`"plan_count":2`,
		`"valid":true`,
		`"diagnostics":[]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}
