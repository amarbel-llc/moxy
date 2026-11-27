package toolbox

import (
	"slices"
	"strings"
	"testing"
)

func TestParseRefusesShellSyntax(t *testing.T) {
	for _, cmd := range []string{
		"wc -c a > b",
		"wc -c < a",
		"wc -c a; rm a",
		"wc -c a && wc b",
		"wc -c $HOME/a",
		"wc -c `x`",
		"wc -c $(x)",
		"wc -c *.txt",
		"wc -c ~/a",
		"wc -c a # comment",
		"wc -c a\nwc b",
		`wc -c a\ b`,
		`wc -c "$HOME"`,
		"wc -c 'unterminated",
		`wc -c "unterminated`,
		"",
		"wc -c a |",
		"| wc",
		"wc a || wc b",
	} {
		if _, err := Parse(cmd); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", cmd)
		}
	}
}

func TestParseRefusesUnknownAppletsAndFlags(t *testing.T) {
	for _, cmd := range []string{
		"cat a",
		"sh -c x",
		"sort -o out a",
		"sort --output=out a",
		"sort -T /tmp a",
		"sort --compress-program=gzip a",
		"sort --files0-from=list",
		"sha256sum -c sums",
		"sha256sum --check sums",
		"uniq a b",
		"wc --files0-from=list",
		"tr",
		"tr a b c",
		"tr a --nope",
		"uniq -f x a",
		"digest -a md5 a",
		"b2sum -l 7 a",
		"wc --bytes=3 a",
		"sort -k",
	} {
		if _, err := Parse(cmd); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", cmd)
		}
	}
}

func TestParsePipelineAndArgs(t *testing.T) {
	p, err := Parse(`sort -rnk 2 --field-separator=: 'a b.txt' "c\"d" | uniq -c | tr -d -- -`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Stages) != 3 {
		t.Fatalf("stages = %d", len(p.Stages))
	}
	sort := p.Stages[0]
	if got, want := sort.Args, []string{"-r", "-n", "-k", "2", "-t", ":"}; !slices.Equal(got, want) {
		t.Errorf("sort args = %q, want %q", got, want)
	}
	if got, want := sort.Operands, []string{"a b.txt", `c"d`}; !slices.Equal(got, want) {
		t.Errorf("sort operands = %q, want %q", got, want)
	}
	if got := p.Stages[2].Operands; !slices.Equal(got, []string{"-"}) {
		t.Errorf("tr operands = %q", got)
	}
	if got, want := p.Files(), []string{"a b.txt", `c"d`}; !slices.Equal(got, want) {
		t.Errorf("Files = %q, want %q", got, want)
	}
}

func TestFilesSkipsValuesAndStdin(t *testing.T) {
	p, err := Parse("wc -c - madder://blobs/blake2b256-abc x | tr a-z A-Z")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Files(), []string{"madder://blobs/blake2b256-abc", "x"}; !slices.Equal(got, want) {
		t.Errorf("Files = %q, want %q", got, want)
	}
}

// moxy substitutes a blob URI wherever it appears in the command, after the
// permission check has read the command. So a file operand that holds one must
// be exactly it: anything around it would be judged as one path and opened as
// another (moxy#535).
func TestFileOperandHoldsABlobURIOnlyWhole(t *testing.T) {
	for _, command := range []string{
		"wc -c ..madder://blobs/blake2b256-abc/../../../x",
		"wc -c madder://blobs/blake2b256-abc/../x",
		"wc -c a/madder://blobs/blake2b256-abc",
		"wc -c madder://blobs/blake2b256-abc'/../x'",
		`wc -c ma''dder://blobs/blake2b256-abc/x`,
		"sort madder://blobs/..",
		"wc -c madder://other/x",
		"tr a b | cut -b 1- x/madder://blobs/blake2b256-abc",
	} {
		_, err := Parse(command)
		if err == nil || !strings.Contains(err.Error(), "its own operand") {
			t.Errorf("Parse(%q) err = %v, want a refusal naming the way on", command, err)
		}
	}
	for _, command := range []string{
		"wc -c madder://blobs/blake2b256-abc",
		"wc -c 'madder://blobs/blake2b256-abc'",
		"wc -c /dev/fd/3",
		// not a file operand: text, whatever it says
		"tr -d 'madder://x'",
	} {
		if _, err := Parse(command); err != nil {
			t.Errorf("Parse(%q): %v", command, err)
		}
	}
}

func TestQuotedMetacharactersAreLiteral(t *testing.T) {
	p, err := Parse(`tr '[:upper:]' '[:lower:]' | tr -d '$;|*'`)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Stages[1].Operands[0]; got != "$;|*" {
		t.Errorf("operand = %q", got)
	}
}

func TestEveryAppletIsDocumentedInItsSummary(t *testing.T) {
	for _, a := range Applets() {
		if strings.TrimSpace(a.Summary) == "" {
			t.Errorf("%s has no summary", a.Name)
		}
	}
}
