// Package toolbox is the grammar and runner behind the toolbox moxin (FDR
// 0016, moxy#500): a command string of pure applets joined by `|`, parsed by
// moxy and never handed to a shell. The same parse tells the permission gate
// which words are file operands, so the gate and the runner cannot disagree
// about what a command reads.
package toolbox

import (
	"fmt"
	"strings"
)

// token is one word of a command, or a pipe.
type token struct {
	text string
	pipe bool
}

// refused are the characters a shell would give meaning to. Unquoted, each
// one is an error rather than a literal, so a command never means something
// different here than it would in sh: there are no redirects, substitutions,
// globs, variables, comments, lists or escapes.
const refused = ";&<>()`$*?[]{}~#!\\\n\r"

// lex splits a command into words and pipes. Words are separated by spaces or
// tabs; '…' quotes literally; "…" quotes with \" and \\ as the only escapes
// and refuses $ and ` (which sh would expand). Adjacent quoted and unquoted
// parts join into one word.
func lex(command string) ([]token, error) {
	var (
		out    []token
		word   strings.Builder
		inWord bool
		runes  = []rune(command)
		flush  = func() {
			if inWord {
				out = append(out, token{text: word.String()})
				word.Reset()
				inWord = false
			}
		}
	)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == ' ' || r == '\t':
			flush()
		case r == '|':
			flush()
			out = append(out, token{pipe: true})
		case r == '\'':
			inWord = true
			end := indexRune(runes, i+1, '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated ' quote")
			}
			word.WriteString(string(runes[i+1 : end]))
			i = end
		case r == '"':
			inWord = true
			i++
			for ; i < len(runes) && runes[i] != '"'; i++ {
				switch c := runes[i]; {
				case c == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
					i++
					word.WriteRune(runes[i])
				case c == '$' || c == '`':
					return nil, fmt.Errorf("%q inside double quotes is not allowed (use single quotes for a literal)", c)
				default:
					word.WriteRune(c)
				}
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("unterminated \" quote")
			}
		case strings.ContainsRune(refused, r):
			return nil, fmt.Errorf("%q is not allowed unquoted: toolbox is not a shell (see toolbox(1))", r)
		default:
			inWord = true
			word.WriteRune(r)
		}
	}
	flush()
	return out, nil
}

func indexRune(runes []rune, from int, r rune) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == r {
			return i
		}
	}
	return -1
}
