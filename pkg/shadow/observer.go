package shadow

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Verdict is an evaluator's answer, kept a string so this package needs no
// ajour types (ajour.Verdict.String() is what an adapter returns).
type Verdict string

// Verdicts the observer itself records when there is no evaluator answer.
const (
	VerdictUnevaluated Verdict = "unevaluated" // no evaluator is plugged in
	VerdictRefused     Verdict = "refused"     // the builder could not present the call
	VerdictError       Verdict = "error"       // the evaluator errored or panicked
)

// Evaluator is where ajour.Evaluate plugs in later.
type Evaluator interface {
	Evaluate(Call) (Verdict, error)
}

// FieldRecord is one presented value in the log: its field and the kind it
// was presented as, never its value.
type FieldRecord struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Record is one log line. It names the tool, the types of the fields and
// the verdict, and carries no argument values.
type Record struct {
	Tool    string        `json:"tool"`
	Fields  []FieldRecord `json:"fields,omitempty"`
	Verdict Verdict       `json:"verdict"`
	Error   string        `json:"error,omitempty"`
}

// Observer evaluates and logs calls in shadow mode. It never affects the
// call it observes: Observe returns nothing and recovers from every panic.
type Observer struct {
	builder *Builder
	eval    Evaluator
	mu      sync.Mutex
	log     io.Writer
}

// NewObserver returns an Observer. eval may be nil (calls are logged
// "unevaluated"); log may be nil (nothing is written).
func NewObserver(b *Builder, eval Evaluator, log io.Writer) *Observer {
	return &Observer{builder: b, eval: eval, log: log}
}

// Observe builds, evaluates and logs one dispatched tool call. A nil
// Observer does nothing.
func (o *Observer) Observe(name string, args map[string]any) {
	if o == nil {
		return
	}
	defer func() { _ = recover() }() // shadow mode never breaks a call

	calls, err := o.builder.BuildNamed(name, args)
	if err != nil {
		// Builder errors name fields, never values.
		o.write(Record{Tool: name, Verdict: VerdictRefused, Error: err.Error()})
		return
	}
	for _, c := range calls {
		o.write(o.evaluate(c))
	}
}

func (o *Observer) evaluate(c Call) (rec Record) {
	rec = Record{Tool: toolName(c), Fields: fieldRecords(c), Verdict: VerdictUnevaluated}
	if o.eval == nil {
		return rec
	}
	defer func() {
		if r := recover(); r != nil {
			rec.Verdict = VerdictError
			rec.Error = fmt.Sprintf("evaluator panic: %v", r)
		}
	}()
	v, err := o.eval.Evaluate(c)
	if err != nil {
		rec.Verdict = VerdictError
		rec.Error = err.Error()
		return rec
	}
	rec.Verdict = v
	return rec
}

func (o *Observer) write(r Record) {
	if o.log == nil {
		return
	}
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.log.Write(append(line, '\n'))
}

func toolName(c Call) string {
	if len(c.Path) < 3 {
		return ""
	}
	if c.Path[1].ID == pluginMoxy {
		return c.Path[2].ID
	}
	return strings.Join([]string{c.Path[1].ID, c.Path[2].ID}, ".")
}

func fieldRecords(c Call) []FieldRecord {
	var out []FieldRecord
	for _, v := range c.Values {
		if len(v) == 0 {
			continue
		}
		name := v[0].ID
		if f := v[0].Fields["field"]; len(f) == 1 {
			name = f[0]
		}
		out = append(out, FieldRecord{Name: name, Type: v[0].Type})
	}
	for _, r := range c.Resources {
		if len(r) == 0 {
			continue
		}
		out = append(out, FieldRecord{Name: "(resource)", Type: r[len(r)-1].Type})
	}
	return out
}
