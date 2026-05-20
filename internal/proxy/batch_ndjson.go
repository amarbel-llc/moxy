package proxy

// NDJSON mirror types matching amarbel-llc/tap pkgs/ndjson schema.
// Defined locally to avoid the module dep; field names and JSON tags
// match exactly so an amarbel-llc/tap consumer can json.Unmarshal
// batch output into their types.

type ndjsonTestRecord struct {
	Type        string             `json:"type"`        // "test"
	N           int                `json:"n"`           // 1-indexed
	Description string             `json:"description"` // tool name
	OK          bool               `json:"ok"`
	Directive   *ndjsonDirective   `json:"directive"`
	Diagnostic  map[string]any     `json:"diagnostic"`
	Output      *string            `json:"output"`
	Subtest     []ndjsonTestRecord `json:"subtest"` // always empty in v1
	Line        int                `json:"line"`    // 1-indexed
}

type ndjsonDirective struct {
	Kind   string `json:"kind"` // "skip" | "todo"
	Reason string `json:"reason"`
}

type ndjsonBailoutRecord struct {
	Type    string `json:"type"` // "bailout"
	Message string `json:"message"`
	Line    int    `json:"line"`
}

type ndjsonSummaryRecord struct {
	Type        string                    `json:"type"` // "summary"
	Passed      int                       `json:"passed"`
	Failed      int                       `json:"failed"`
	Skipped     int                       `json:"skipped"`
	Todo        int                       `json:"todo"`
	Total       int                       `json:"total"`
	PlanCount   int                       `json:"plan_count"`
	Bailed      bool                      `json:"bailed"`
	Valid       bool                      `json:"valid"`
	Diagnostics []ndjsonSummaryDiagnostic `json:"diagnostics"`
}

type ndjsonSummaryDiagnostic struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
