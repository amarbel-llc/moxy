package toolbox

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxStringBytes bounds what one string stage reads from its input and what
// it writes, so a pattern or a count cannot make it hold or emit unbounded
// data.
const MaxStringBytes = 16 << 20

// stringApplet is fish's `string` builtin as one in-process applet (moxy#502,
// #498 folded in): pure, no files, no exec, RE2 regular expressions. Its
// operands are strings, not paths, like tr's sets; with none, it reads its
// input's lines.
var stringApplet = &Applet{
	Name:    "string",
	Summary: "length, sub, upper, lower, trim, split, join, replace, match, pad, repeat, collapse, escape, unescape",
	Subcommands: []*Applet{
		stringSub("length", nil, 0, nil),
		stringSub("sub", []Flag{
			{Short: 's', Long: "start", Value: true, Valid: signedNonZero},
			{Short: 'l', Long: "length", Value: true, Valid: digits},
			{Short: 'e', Long: "end", Value: true, Valid: signedNonZero},
		}, 0, func(st Stage) error {
			_, hasLength := st.Flags["length"]
			_, hasEnd := st.Flags["end"]
			if hasLength && hasEnd {
				return errors.New("-l and -e are exclusive")
			}
			return nil
		}),
		stringSub("upper", nil, 0, nil),
		stringSub("lower", nil, 0, nil),
		stringSub("trim", []Flag{
			{Short: 'l', Long: "left"},
			{Short: 'r', Long: "right"},
			{Short: 'c', Long: "chars", Value: true},
		}, 0, nil),
		stringSub("split", []Flag{
			{Short: 'm', Long: "max", Value: true, Valid: digits},
			{Short: 'r', Long: "right"},
			{Short: 'n', Long: "no-empty"},
		}, 1, nil),
		stringSub("join", nil, 1, nil),
		stringSub("replace", []Flag{
			{Short: 'a', Long: "all"},
			{Short: 'r', Long: "regex"},
			{Short: 'f', Long: "filter"},
		}, 2, compilesIfRegex),
		stringSub("match", []Flag{
			{Short: 'a', Long: "all"},
			{Short: 'e', Long: "entire"},
			{Short: 'r', Long: "regex"},
			{Short: 'v', Long: "invert"},
			{Short: 'g', Long: "groups-only"},
		}, 1, compilesIfRegex),
		stringSub("pad", []Flag{
			{Short: 'w', Long: "width", Value: true, Valid: digits},
			{Short: 'c', Long: "char", Value: true, Valid: oneRune},
			{Short: 'r', Long: "right"},
		}, 0, nil),
		stringSub("repeat", []Flag{
			{Short: 'n', Long: "count", Value: true, Valid: digits},
			{Short: 'm', Long: "max", Value: true, Valid: digits},
			{Short: 'N', Long: "no-newline"},
		}, 0, func(st Stage) error {
			_, hasCount := st.Flags["count"]
			_, hasMax := st.Flags["max"]
			if !hasCount && !hasMax {
				return errors.New("needs -n or -m")
			}
			return nil
		}),
		stringSub("collapse", nil, 0, nil),
		stringSub("escape", escapeFlags(), 0, checkEscapeFlags),
		stringSub("unescape", escapeFlags(), 0, checkEscapeFlags),
	},
}

func stringSub(name string, flags []Flag, positional int, check func(Stage) error) *Applet {
	return &Applet{
		Name:        "string " + name,
		Summary:     "string " + name,
		Flags:       flags,
		Page:        "string",
		Positional:  positional,
		MinOperands: positional,
		MaxOperands: -1,
		Check:       check,
	}
}

func compilesIfRegex(st Stage) error {
	if _, ok := st.Flags["regex"]; !ok {
		return nil
	}
	_, err := regexp.Compile(st.Operands[0])
	return err
}

// cappedWriter fails a write that would take the stage past MaxStringBytes.
type cappedWriter struct {
	w    *bufio.Writer
	left int
}

var errOutputCap = fmt.Errorf("output would exceed %d bytes", MaxStringBytes)

func (c *cappedWriter) WriteString(s string) error {
	if len(s) > c.left {
		return errOutputCap
	}
	c.left -= len(s)
	_, err := c.w.WriteString(s)
	return err
}

func (c *cappedWriter) line(s string) error {
	if err := c.WriteString(s); err != nil {
		return err
	}
	return c.WriteString("\n")
}

func stringInputs(st Stage, in io.Reader) ([]string, error) {
	if rest := st.Operands[st.Applet.Positional:]; len(rest) > 0 {
		return rest, nil
	}
	data, err := io.ReadAll(io.LimitReader(in, MaxStringBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxStringBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", MaxStringBytes)
	}
	text := strings.TrimSuffix(string(data), "\n")
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func runString(st Stage, in io.Reader, out io.Writer) error {
	inputs, err := stringInputs(st, in)
	if err != nil {
		return fmt.Errorf("%s: %w", st.Applet.Name, err)
	}
	w := &cappedWriter{w: bufio.NewWriter(out), left: MaxStringBytes}
	if err := stringOp(st, inputs, w); err != nil {
		return fmt.Errorf("%s: %w", st.Applet.Name, err)
	}
	return w.w.Flush()
}

func intFlag(st Stage, name string) (int, bool) {
	v, ok := st.Flags[name]
	if !ok {
		return 0, false
	}
	n, _ := strconv.Atoi(v) // validated at parse time
	return n, true
}

func stringOp(st Stage, inputs []string, w *cappedWriter) error {
	flag := func(name string) bool { _, ok := st.Flags[name]; return ok }
	args := st.Operands[:st.Applet.Positional]
	switch strings.TrimPrefix(st.Applet.Name, "string ") {
	case "length":
		for _, s := range inputs {
			if err := w.line(strconv.Itoa(utf8.RuneCountInString(s))); err != nil {
				return err
			}
		}
	case "upper", "lower":
		conv := strings.ToUpper
		if st.Applet.Name == "string lower" {
			conv = strings.ToLower
		}
		for _, s := range inputs {
			if err := w.line(conv(s)); err != nil {
				return err
			}
		}
	case "sub":
		for _, s := range inputs {
			if err := w.line(substring(st, s)); err != nil {
				return err
			}
		}
	case "trim":
		cutset := " \t\n\r\v\f"
		if c, ok := st.Flags["chars"]; ok {
			cutset = c
		}
		left, right := flag("left"), flag("right")
		if !left && !right {
			left, right = true, true
		}
		for _, s := range inputs {
			if left {
				s = strings.TrimLeft(s, cutset)
			}
			if right {
				s = strings.TrimRight(s, cutset)
			}
			if err := w.line(s); err != nil {
				return err
			}
		}
	case "split":
		maxSplits, limited := intFlag(st, "max")
		for _, s := range inputs {
			for _, part := range split(s, args[0], maxSplits, limited, flag("right")) {
				if part == "" && flag("no-empty") {
					continue
				}
				if err := w.line(part); err != nil {
					return err
				}
			}
		}
	case "join":
		if len(inputs) > 0 {
			return w.line(strings.Join(inputs, args[0]))
		}
	case "replace":
		return replace(st, args[0], args[1], inputs, w)
	case "match":
		return match(st, args[0], inputs, w)
	case "pad":
		return pad(st, inputs, w)
	case "repeat":
		return repeat(st, inputs, w)
	case "collapse":
		for _, s := range inputs {
			if err := w.line(collapse(s)); err != nil {
				return err
			}
		}
	case "escape", "unescape":
		return escapeLines(st, st.Applet.Name == "string unescape", inputs, w)
	default:
		return errors.New("no implementation")
	}
	return nil
}

// substring takes fish's 1-based -s (negative counts from the end) with
// either -l, a length, or -e, an inclusive end that may also be negative.
func substring(st Stage, s string) string {
	r := []rune(s)
	n := len(r)
	start := 1
	if v, ok := intFlag(st, "start"); ok {
		start = v
		if v < 0 {
			start = n + v + 1
		}
	}
	from := min(max(start, 1)-1, n)
	to := n
	if v, ok := intFlag(st, "end"); ok {
		to = v
		if v < 0 {
			to = n + v + 1
		}
	} else if v, ok := intFlag(st, "length"); ok {
		to = from + v
	}
	to = min(max(to, from), n)
	return string(r[from:to])
}

func split(s, sep string, maxSplits int, limited, fromRight bool) []string {
	parts := strings.Split(s, sep)
	if !limited || len(parts) <= maxSplits+1 {
		return parts
	}
	if fromRight {
		k := len(parts) - maxSplits
		return append([]string{strings.Join(parts[:k], sep)}, parts[k:]...)
	}
	return append(parts[:maxSplits:maxSplits], strings.Join(parts[maxSplits:], sep))
}

func replace(st Stage, pattern, replacement string, inputs []string, w *cappedWriter) error {
	_, all := st.Flags["all"]
	_, filter := st.Flags["filter"]
	var re *regexp.Regexp
	if _, ok := st.Flags["regex"]; ok {
		re = regexp.MustCompile(pattern) // compiled at parse time
	}
	for _, s := range inputs {
		var got string
		switch {
		case re == nil && all:
			got = strings.ReplaceAll(s, pattern, replacement)
		case re == nil:
			got = strings.Replace(s, pattern, replacement, 1)
		case all:
			got = re.ReplaceAllString(s, replacement)
		default:
			got = s
			if loc := re.FindStringSubmatchIndex(s); loc != nil {
				got = s[:loc[0]] + string(re.ExpandString(nil, replacement, s, loc)) + s[loc[1]:]
			}
		}
		if filter && got == s {
			continue
		}
		if err := w.line(got); err != nil {
			return err
		}
	}
	return nil
}

// globRegexp is fish's glob: * and ? over the whole string, anything else
// literal.
func globRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString(`(?s)^`)
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(`$`)
	return regexp.MustCompile(b.String())
}

func match(st Stage, pattern string, inputs []string, w *cappedWriter) error {
	flag := func(name string) bool { _, ok := st.Flags[name]; return ok }
	if !flag("regex") {
		re := globRegexp(pattern)
		for _, s := range inputs {
			if re.MatchString(s) != flag("invert") {
				if err := w.line(s); err != nil {
					return err
				}
			}
		}
		return nil
	}
	re := regexp.MustCompile(pattern) // compiled at parse time
	for _, s := range inputs {
		n := 1
		if flag("all") {
			n = -1
		}
		found := re.FindAllStringSubmatchIndex(s, n)
		switch {
		case flag("invert"):
			if found == nil {
				if err := w.line(s); err != nil {
					return err
				}
			}
			continue
		case found == nil:
			continue
		case flag("entire"):
			if err := w.line(s); err != nil {
				return err
			}
			continue
		}
		for _, loc := range found {
			for g := 0; g < len(loc)/2; g++ {
				if (g == 0 && flag("groups-only")) || loc[2*g] < 0 {
					continue
				}
				if err := w.line(s[loc[2*g]:loc[2*g+1]]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func pad(st Stage, inputs []string, w *cappedWriter) error {
	width, _ := intFlag(st, "width")
	if width > MaxStringBytes {
		return errOutputCap
	}
	char := " "
	if c, ok := st.Flags["char"]; ok {
		char = c
	}
	_, right := st.Flags["right"]
	for _, s := range inputs {
		fill := strings.Repeat(char, max(width-utf8.RuneCountInString(s), 0))
		if right {
			s += fill
		} else {
			s = fill + s
		}
		if err := w.line(s); err != nil {
			return err
		}
	}
	return nil
}

func repeat(st Stage, inputs []string, w *cappedWriter) error {
	count, hasCount := intFlag(st, "count")
	maxRunes, hasMax := intFlag(st, "max")
	_, noNewline := st.Flags["no-newline"]
	for _, s := range inputs {
		if s != "" {
			r := []rune(s)
			left := maxRunes
			for i := 0; !hasCount || i < count; i++ {
				chunk := r
				if hasMax {
					if left <= 0 {
						break
					}
					chunk = r[:min(len(r), left)]
					left -= len(chunk)
				}
				if err := w.WriteString(string(chunk)); err != nil {
					return err
				}
			}
		}
		if !noNewline {
			if err := w.WriteString("\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func collapse(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}
