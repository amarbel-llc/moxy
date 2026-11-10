package shadow

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeEvaluator struct {
	verdict Verdict
	err     error
	panics  bool
	seen    []Call
}

func (f *fakeEvaluator) Evaluate(c Call) (Verdict, error) {
	f.seen = append(f.seen, c)
	if f.panics {
		panic("evaluator exploded")
	}
	return f.verdict, f.err
}

func logLines(t *testing.T, buf *bytes.Buffer) []Record {
	t.Helper()
	var out []Record
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

func TestObserveLogsToolFieldTypesAndVerdictNotValues(t *testing.T) {
	var buf bytes.Buffer
	ev := &fakeEvaluator{verdict: "deny"}
	o := NewObserver(newTestBuilder(t, testEnv()), ev, &buf)

	const secret = "s3cr3t-token-value"
	o.Observe("folio.read", map[string]any{"file_path": "/home/u/" + secret, "start": 1.0})
	o.Observe("rg.search", map[string]any{"pattern": secret})

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("log leaks a value: %s", buf.String())
	}
	recs := logLines(t, &buf)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	r := recs[0]
	if r.Tool != "folio.read" || r.Verdict != "deny" {
		t.Errorf("record = %+v", r)
	}
	types := map[string]string{}
	for _, f := range r.Fields {
		types[f.Name] = f.Type
	}
	if types["file_path"] != "fs-path-v1" || types["start"] != "tool-input-v1" {
		t.Errorf("field types = %v", types)
	}
	if len(ev.seen) != 2 {
		t.Errorf("evaluator saw %d calls, want 2", len(ev.seen))
	}
}

func TestObserveWithNoEvaluatorRecordsUnevaluated(t *testing.T) {
	var buf bytes.Buffer
	o := NewObserver(newTestBuilder(t, testEnv()), nil, &buf)
	o.Observe("man.toc", map[string]any{"page": "ls"})
	recs := logLines(t, &buf)
	if len(recs) != 1 || recs[0].Verdict != VerdictUnevaluated {
		t.Fatalf("records = %+v", recs)
	}
}

func TestObserveRecordsBuilderRefusalWithoutValues(t *testing.T) {
	var buf bytes.Buffer
	ev := &fakeEvaluator{verdict: "allow"}
	o := NewObserver(newTestBuilder(t, testEnv()), ev, &buf)
	const secret = "/etc/very-secret-dir/*"
	o.Observe("folio.glob", map[string]any{"pattern": secret})
	if strings.Contains(buf.String(), "very-secret-dir") {
		t.Fatalf("log leaks the refused pattern: %s", buf.String())
	}
	recs := logLines(t, &buf)
	if len(recs) != 1 || recs[0].Verdict != VerdictRefused || recs[0].Error == "" {
		t.Fatalf("records = %+v", recs)
	}
	if len(ev.seen) != 0 {
		t.Errorf("evaluator ran on a call the builder refused")
	}
}

func TestObserveSurvivesEvaluatorErrorAndPanic(t *testing.T) {
	var buf bytes.Buffer
	o := NewObserver(newTestBuilder(t, testEnv()), &fakeEvaluator{err: errors.New("bad layer")}, &buf)
	o.Observe("man.toc", map[string]any{"page": "ls"})
	if recs := logLines(t, &buf); len(recs) != 1 || recs[0].Verdict != VerdictError {
		t.Fatalf("error case records = %+v", recs)
	}

	buf.Reset()
	o = NewObserver(newTestBuilder(t, testEnv()), &fakeEvaluator{panics: true}, &buf)
	o.Observe("man.toc", map[string]any{"page": "ls"}) // must not panic
	if recs := logLines(t, &buf); len(recs) != 1 || recs[0].Verdict != VerdictError {
		t.Fatalf("panic case records = %+v", recs)
	}
}

func TestObserveOnNilObserverIsANoop(t *testing.T) {
	var o *Observer
	o.Observe("man.toc", map[string]any{"page": "ls"}) // must not panic
}
