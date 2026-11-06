package toolbox

import (
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"
)

// Flag is one option an applet accepts. Value flags take exactly one argument
// (`-k V`, `-kV`, `--key V` or `--key=V`); the rest are booleans.
type Flag struct {
	Short byte   // 0 when the flag has no short form
	Long  string // "" when the flag has no long form
	Value bool
	// Valid checks a Value flag's argument; nil accepts any.
	Valid func(string) error
}

// spelling is how the flag is passed to the program: its short form when it
// has one, else its long form.
func (f Flag) spelling() string {
	if f.Short != 0 {
		return "-" + string(f.Short)
	}
	return "--" + f.Long
}

// Applet is one program toolbox serves. Its flags are the whole of what a
// command may pass it: anything that writes, executes, or reads a file the
// gate never saw (sort -o/-T/--compress-program/--files0-from, sha256sum -c,
// uniq's output operand) is absent, so it cannot be reached.
type Applet struct {
	Name    string
	Summary string
	// Program is the coreutils program run for this applet; "" means the
	// applet runs in-process (digest, string, strings).
	Program string
	Flags   []Flag
	// FileOperands is true when operands name inputs (files, blob URIs, or
	// "-" for stdin); false when they are plain values (tr's sets).
	FileOperands bool
	MinOperands  int
	MaxOperands  int // -1: unbounded
	// Subcommands, when set, make the first word after the applet's name a
	// subcommand, parsed as an applet of its own (fish's `string trim ...`).
	// The policy grammar parses with this same code, so it judges exactly
	// the operands the runner sees.
	Subcommands []*Applet
	// Page names the toolbox-<page>(1) that usage errors point to; the
	// applet's own name when "".
	Page string
	// Positional is how many leading operands are arguments, not inputs
	// (string split's separator).
	Positional int
	// Check validates a parsed stage beyond its flag table, such as a
	// regular expression that must compile.
	Check func(Stage) error
}

func (a *Applet) page() string {
	if a.Page != "" {
		return a.Page
	}
	return a.Name
}

func (a *Applet) subcommand(name string) (*Applet, bool) {
	for _, s := range a.Subcommands {
		if s.Name == a.Name+" "+name {
			return s, true
		}
	}
	return nil, false
}

func signedNonZero(v string) error {
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n == 0 {
		return fmt.Errorf("want a non-zero integer, got %q", v)
	}
	return nil
}

func oneRune(v string) error {
	if utf8.RuneCountInString(v) != 1 {
		return fmt.Errorf("want exactly one character, got %q", v)
	}
	return nil
}

func digits(v string) error {
	if _, err := strconv.ParseUint(v, 10, 32); err != nil {
		return fmt.Errorf("want a non-negative integer, got %q", v)
	}
	return nil
}

func oneOf(values ...string) func(string) error {
	return func(v string) error {
		if !slices.Contains(values, v) {
			return fmt.Errorf("want one of %v, got %q", values, v)
		}
		return nil
	}
}

func blake2bLength(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || n > 512 || n%8 != 0 {
		return fmt.Errorf("want a multiple of 8 from 8 to 512, got %q", v)
	}
	return nil
}

// Algorithms digest accepts: the markl digest formats (markl-id(7)).
var Algorithms = []string{"sha256", "blake2b256"}

var applets = []*Applet{
	{
		Name:    "b2sum",
		Summary: "BLAKE2b checksums",
		Program: "b2sum",
		Flags: []Flag{
			{Short: 'l', Long: "length", Value: true, Valid: blake2bLength},
			{Long: "tag"},
			{Short: 'z', Long: "zero"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	{
		Name:    "cut",
		Summary: "select fields, characters or bytes of each line",
		Program: "cut",
		Flags: []Flag{
			{Short: 'b', Long: "bytes", Value: true},
			{Short: 'c', Long: "characters", Value: true},
			{Short: 'f', Long: "fields", Value: true},
			{Short: 'd', Long: "delimiter", Value: true},
			{Short: 's', Long: "only-delimited"},
			{Long: "complement"},
			{Long: "output-delimiter", Value: true},
			{Short: 'z', Long: "zero-terminated"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	{
		Name:    "digest",
		Summary: "markl digest ids (sha256, blake2b256)",
		Flags: []Flag{
			{Short: 'a', Long: "algorithm", Value: true, Valid: oneOf(Algorithms...)},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	{
		Name:    "paste",
		Summary: "merge lines of inputs",
		Program: "paste",
		Flags: []Flag{
			{Short: 'd', Long: "delimiters", Value: true},
			{Short: 's', Long: "serial"},
			{Short: 'z', Long: "zero-terminated"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	{
		Name:    "sha256sum",
		Summary: "SHA-256 checksums",
		Program: "sha256sum",
		Flags: []Flag{
			{Long: "tag"},
			{Short: 'z', Long: "zero"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	{
		Name:    "sort",
		Summary: "sort lines",
		Program: "sort",
		Flags: []Flag{
			{Short: 'b', Long: "ignore-leading-blanks"},
			{Short: 'c', Long: "check"},
			{Short: 'C'},
			{Short: 'd', Long: "dictionary-order"},
			{Short: 'f', Long: "ignore-case"},
			{Short: 'g', Long: "general-numeric-sort"},
			{Short: 'h', Long: "human-numeric-sort"},
			{Short: 'i', Long: "ignore-nonprinting"},
			{Short: 'k', Long: "key", Value: true},
			{Short: 'm', Long: "merge"},
			{Short: 'M', Long: "month-sort"},
			{Short: 'n', Long: "numeric-sort"},
			{Short: 'r', Long: "reverse"},
			{Short: 's', Long: "stable"},
			{Short: 't', Long: "field-separator", Value: true},
			{Short: 'u', Long: "unique"},
			{Short: 'V', Long: "version-sort"},
			{Short: 'z', Long: "zero-terminated"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
	stringApplet,
	stringsApplet,
	{
		Name:    "tr",
		Summary: "translate, squeeze or delete characters",
		Program: "tr",
		Flags: []Flag{
			{Short: 'c', Long: "complement"},
			{Short: 'C'},
			{Short: 'd', Long: "delete"},
			{Short: 's', Long: "squeeze-repeats"},
			{Short: 't', Long: "truncate-set1"},
		},
		MinOperands: 1,
		MaxOperands: 2,
	},
	{
		Name:    "uniq",
		Summary: "report or omit repeated adjacent lines",
		Program: "uniq",
		Flags: []Flag{
			{Short: 'c', Long: "count"},
			{Short: 'd', Long: "repeated"},
			{Short: 'u', Long: "unique"},
			{Short: 'i', Long: "ignore-case"},
			{Short: 'f', Long: "skip-fields", Value: true, Valid: digits},
			{Short: 's', Long: "skip-chars", Value: true, Valid: digits},
			{Short: 'w', Long: "check-chars", Value: true, Valid: digits},
			{Short: 'z', Long: "zero-terminated"},
		},
		FileOperands: true,
		// uniq's second operand is an output file.
		MaxOperands: 1,
	},
	{
		Name:    "wc",
		Summary: "count lines, words, characters and bytes",
		Program: "wc",
		Flags: []Flag{
			{Short: 'c', Long: "bytes"},
			{Short: 'm', Long: "chars"},
			{Short: 'l', Long: "lines"},
			{Short: 'w', Long: "words"},
			{Short: 'L', Long: "max-line-length"},
		},
		FileOperands: true,
		MaxOperands:  -1,
	},
}

// Applets returns every applet, sorted by name.
func Applets() []*Applet { return applets }

// Lookup returns the applet with that name.
func Lookup(name string) (*Applet, bool) {
	for _, a := range applets {
		if a.Name == name {
			return a, true
		}
	}
	return nil, false
}

func (a *Applet) short(c byte) (Flag, bool) {
	for _, f := range a.Flags {
		if f.Short != 0 && f.Short == c {
			return f, true
		}
	}
	return Flag{}, false
}

func (a *Applet) long(name string) (Flag, bool) {
	for _, f := range a.Flags {
		if f.Long != "" && f.Long == name {
			return f, true
		}
	}
	return Flag{}, false
}
