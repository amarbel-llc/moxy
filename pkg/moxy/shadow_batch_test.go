package moxy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/pkg/shadow"
)

// Shadow mode counts what review uses to decide what can be promoted, so a
// batch of two calls must yield exactly two entries, each marked as having run
// inside a batch (moxy#523: they were logged twice, once as the batch was
// presented and once as each inner call ran).
func TestTwoCallBatchYieldsTwoShadowEntries(t *testing.T) {
	var log bytes.Buffer
	dir := t.TempDir()
	builder, err := shadow.NewBuilder(shadow.Env{Cwd: dir, Home: dir})
	if err != nil {
		t.Fatal(err)
	}
	obs := shadow.NewAsync(shadow.NewObserver(builder, nil, &log), 64)

	inst := openFixture(t, func(o *Options) {
		o.MetaTools = true
		o.Shadow = obs
	})
	res, err := inst.Call(context.Background(), Call{
		Name: "batch",
		Args: json.RawMessage(`{"calls":[
			{"tool":"fix.echo","args":{"text":"one"}},
			{"tool":"fix.echo","args":{"text":"two"}}]}`),
	})
	if err != nil || res.IsError {
		t.Fatalf("batch: res=%+v err=%v", res, err)
	}
	obs.Close() // drains the queue before the log is read

	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("shadow line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	if len(recs) != 2 {
		t.Fatalf("a two-call batch must log two shadow entries, got %d:\n%s", len(recs), log.String())
	}
	for i, rec := range recs {
		if rec["tool"] != "fix.echo" || rec["via"] != "batch" {
			t.Errorf("entry %d = %v, want tool fix.echo via batch", i, rec)
		}
	}
}
