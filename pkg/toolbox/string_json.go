package toolbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"
)

// JSON output (moxy#506): `split --json` and `match --json` print one JSON
// value per input, one per line, where the structure carries what text loses
// (empty pieces, groups that did not take part, where a match is). Keys have
// a fixed order, so the output is deterministic. length needs no flag: its
// text is already a JSON number per line.

// jsonMatch is one match of `match --json`. Offsets count code points from 0,
// the end exclusive, like the rest of `string`.
type jsonMatch struct {
	Start  int          `json:"start"`
	End    int          `json:"end"`
	Match  string       `json:"match"`
	Groups []*jsonGroup `json:"groups"`
}

// jsonGroup is a capture group; a group that did not take part is null.
type jsonGroup struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Value string `json:"value"`
}

var errNotUTF8 = errors.New("input is not valid UTF-8, which JSON output cannot hold")

// jsonLine writes v as one line of compact JSON.
func (c *cappedWriter) jsonLine(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return c.WriteString(buf.String())
}

// checkMatch is match's parse-time check: --json prints every match and its
// groups, so the flags that choose what to print do not combine with it.
func checkMatch(st Stage) error {
	if _, ok := st.Flags["json"]; ok {
		for _, name := range []string{"invert", "entire", "groups-only"} {
			if _, set := st.Flags[name]; set {
				return errors.New("--json does not combine with -v, -e or -g")
			}
		}
	}
	return compilesIfRegex(st)
}

// runeOffsets turns byte offsets in a valid UTF-8 string into code point
// offsets. An ASCII string is its own answer; otherwise the offset of every
// character start is computed once, so a million matches do not each rescan
// the prefix. A match never starts or ends inside a character.
type runeOffsets struct {
	ascii bool
	index []int32
}

func newRuneOffsets(s string) *runeOffsets {
	if utf8.RuneCountInString(s) == len(s) {
		return &runeOffsets{ascii: true}
	}
	index := make([]int32, len(s)+1)
	n := int32(0)
	for i := range s {
		index[i] = n
		n++
	}
	index[len(s)] = n
	return &runeOffsets{index: index}
}

func (r *runeOffsets) at(b int) int {
	if r.ascii {
		return b
	}
	return int(r.index[b])
}

func matchJSON(st Stage, pattern string, inputs []string, w *cappedWriter) error {
	var re *regexp.Regexp
	n := 1
	if _, regex := st.Flags["regex"]; regex {
		re = regexp.MustCompile(pattern) // compiled at parse time
		if _, all := st.Flags["all"]; all {
			n = -1
		}
	} else {
		re = globRegexp(pattern)
	}
	for _, s := range inputs {
		if !utf8.ValidString(s) {
			return errNotUTF8
		}
		offsets := newRuneOffsets(s)
		matches := []jsonMatch{}
		for _, loc := range re.FindAllStringSubmatchIndex(s, n) {
			m := jsonMatch{
				Start:  offsets.at(loc[0]),
				End:    offsets.at(loc[1]),
				Match:  s[loc[0]:loc[1]],
				Groups: []*jsonGroup{},
			}
			for g := 1; g < len(loc)/2; g++ {
				if loc[2*g] < 0 {
					m.Groups = append(m.Groups, nil)
					continue
				}
				m.Groups = append(m.Groups, &jsonGroup{
					Start: offsets.at(loc[2*g]),
					End:   offsets.at(loc[2*g+1]),
					Value: s[loc[2*g]:loc[2*g+1]],
				})
			}
			matches = append(matches, m)
		}
		if err := w.jsonLine(matches); err != nil {
			return err
		}
	}
	return nil
}

func splitJSON(st Stage, sep string, inputs []string, w *cappedWriter) error {
	maxSplits, limited := intFlag(st, "max")
	_, fromRight := st.Flags["right"]
	_, noEmpty := st.Flags["no-empty"]
	for _, s := range inputs {
		if !utf8.ValidString(s) {
			return errNotUTF8
		}
		pieces := []string{}
		for _, part := range split(s, sep, maxSplits, limited, fromRight) {
			if part == "" && noEmpty {
				continue
			}
			pieces = append(pieces, part)
		}
		if err := w.jsonLine(pieces); err != nil {
			return err
		}
	}
	return nil
}
