package toolbox

import (
	"fmt"
	"strings"
)

// Stage is one applet invocation in a pipeline.
type Stage struct {
	Applet *Applet
	// Args are the validated flags in the spelling passed to the program:
	// each flag, then its value as a separate word when it takes one.
	Args []string
	// Flags maps each flag given (by long name, else short) to its value
	// ("" for a boolean), for in-process applets.
	Flags map[string]string
	// Operands are the operands as written. For an applet with file
	// operands, each is a path, a madder://blobs/<digest> URI, a /dev/fd/N
	// (the pipe moxy substitutes a blob URI with) or "-" for stdin.
	Operands []string
}

// Pipeline is a parsed command: stages joined by `|`, the first reading the
// command's stdin, each later one the previous one's output.
type Pipeline struct {
	Stages []Stage
}

// Parse parses a command. Every word is checked against its applet's flag
// table; an unknown applet or flag, a bad value or a wrong operand count is
// an error, and so is anything the lexer refuses.
func Parse(command string) (*Pipeline, error) {
	tokens, err := lex(command)
	if err != nil {
		return nil, err
	}
	var (
		p    Pipeline
		cur  []string
		emit = func() error {
			if len(cur) == 0 {
				return fmt.Errorf("empty pipeline stage")
			}
			st, err := parseStage(cur)
			if err != nil {
				return err
			}
			p.Stages = append(p.Stages, st)
			cur = nil
			return nil
		}
	)
	for _, t := range tokens {
		if t.pipe {
			if err := emit(); err != nil {
				return nil, err
			}
			continue
		}
		cur = append(cur, t.text)
	}
	if err := emit(); err != nil {
		return nil, err
	}
	return &p, nil
}

func parseStage(words []string) (Stage, error) {
	a, ok := Lookup(words[0])
	if !ok {
		return Stage{}, fmt.Errorf("%q is not a toolbox applet (have: %s)", words[0], strings.Join(names(), ", "))
	}
	st := Stage{Applet: a, Flags: map[string]string{}}
	add := func(f Flag, value string) error {
		if f.Value && f.Valid != nil {
			if err := f.Valid(value); err != nil {
				return fmt.Errorf("%s %s: %w", a.Name, f.spelling(), err)
			}
		}
		st.Args = append(st.Args, f.spelling())
		if f.Value {
			st.Args = append(st.Args, value)
		}
		key := f.Long
		if key == "" {
			key = string(f.Short)
		}
		st.Flags[key] = value
		return nil
	}
	endOfFlags := false
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case endOfFlags || w == "-" || !strings.HasPrefix(w, "-"):
			st.Operands = append(st.Operands, w)
		case w == "--":
			endOfFlags = true
		case strings.HasPrefix(w, "--"):
			name, value, hasValue := strings.Cut(w[2:], "=")
			f, ok := a.long(name)
			if !ok {
				return Stage{}, fmt.Errorf("%s: unknown flag --%s (see toolbox-%s(1))", a.Name, name, a.Name)
			}
			switch {
			case !f.Value && hasValue:
				return Stage{}, fmt.Errorf("%s: --%s takes no value", a.Name, name)
			case f.Value && !hasValue:
				if i+1 >= len(words) {
					return Stage{}, fmt.Errorf("%s: --%s needs a value", a.Name, name)
				}
				i++
				value = words[i]
			}
			if err := add(f, value); err != nil {
				return Stage{}, err
			}
		default:
			cluster := w[1:]
			for j := 0; j < len(cluster); j++ {
				f, ok := a.short(cluster[j])
				if !ok {
					return Stage{}, fmt.Errorf("%s: unknown flag -%c (see toolbox-%s(1))", a.Name, cluster[j], a.Name)
				}
				if !f.Value {
					if err := add(f, ""); err != nil {
						return Stage{}, err
					}
					continue
				}
				value := cluster[j+1:]
				if value == "" {
					if i+1 >= len(words) {
						return Stage{}, fmt.Errorf("%s: -%c needs a value", a.Name, f.Short)
					}
					i++
					value = words[i]
				}
				if err := add(f, value); err != nil {
					return Stage{}, err
				}
				break
			}
		}
	}
	n := len(st.Operands)
	if n < a.MinOperands || (a.MaxOperands >= 0 && n > a.MaxOperands) {
		return Stage{}, fmt.Errorf("%s: %d operands given, want %s", a.Name, n, operandRange(a))
	}
	return st, nil
}

func operandRange(a *Applet) string {
	switch {
	case a.MaxOperands < 0:
		return fmt.Sprintf("at least %d", a.MinOperands)
	case a.MinOperands == a.MaxOperands:
		return fmt.Sprint(a.MinOperands)
	}
	return fmt.Sprintf("%d to %d", a.MinOperands, a.MaxOperands)
}

func names() []string {
	out := make([]string, len(applets))
	for i, a := range applets {
		out[i] = a.Name
	}
	return out
}

// Files returns every file operand the command reads, in order: paths and
// madder://blobs/<digest> URIs. "-" (stdin) is not one.
func (p *Pipeline) Files() []string {
	var out []string
	for _, st := range p.Stages {
		if !st.Applet.FileOperands {
			continue
		}
		for _, o := range st.Operands {
			if o != "-" {
				out = append(out, o)
			}
		}
	}
	return out
}
