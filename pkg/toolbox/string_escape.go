package toolbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The escape styles of `string escape` and `string unescape` (moxy#505).
const (
	styleShell = "shell"
	styleURL   = "url"
	styleJSON  = "json"
)

// escapeFlags are the flags both escape subcommands take: --style (shell by
// default) and --quotes, which keeps a JSON string literal's quotes.
func escapeFlags() []Flag {
	return []Flag{
		{Long: "style", Value: true, Valid: oneOf(styleShell, styleURL, styleJSON)},
		{Long: "quotes"},
	}
}

// escapeStyle is the style a stage runs with.
func escapeStyle(st Stage) string {
	if s, ok := st.Flags["style"]; ok {
		return s
	}
	return styleShell
}

func checkEscapeFlags(st Stage) error {
	if _, quotes := st.Flags["quotes"]; quotes && escapeStyle(st) != styleJSON {
		return errors.New("--quotes applies to --style=json only")
	}
	return nil
}

// escapeLines writes each input escaped (or unescaped) in the stage's style.
func escapeLines(st Stage, unescape bool, inputs []string, w *cappedWriter) error {
	style := escapeStyle(st)
	_, quotes := st.Flags["quotes"]
	for _, s := range inputs {
		var out string
		var err error
		if unescape {
			out, err = unescapeString(style, quotes, s)
		} else {
			out, err = escapeString(style, quotes, s)
		}
		if err != nil {
			return err
		}
		if err := w.line(out); err != nil {
			return err
		}
	}
	return nil
}

func escapeString(style string, quotes bool, s string) (string, error) {
	switch style {
	case styleURL:
		return escapeURL(s), nil
	case styleJSON:
		return escapeJSON(s, quotes)
	}
	// One single-quoted sh word: a quote inside is closed, escaped and
	// reopened.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

func unescapeString(style string, quotes bool, s string) (string, error) {
	switch style {
	case styleURL:
		return unescapeURL(s)
	case styleJSON:
		return unescapeJSON(s, quotes)
	}
	return unescapeShell(s)
}

// isUnreserved is RFC 3986's unreserved set: the bytes percent-encoding
// leaves as they are.
func isUnreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

const upperHex = "0123456789ABCDEF"

// escapeURL percent-encodes every byte but the unreserved ones, so any bytes,
// valid UTF-8 or not, encode.
func escapeURL(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0xf])
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// unescapeURL decodes %XX escapes. A '+' stays a plus: this is percent
// decoding, not form decoding. The result is whatever bytes were encoded.
func unescapeURL(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("invalid percent escape at byte %d: want %%XX", i)
		}
		hi, okHi := unhex(s[i+1])
		lo, okLo := unhex(s[i+2])
		if !okHi || !okLo {
			return "", fmt.Errorf("invalid percent escape at byte %d: want %%XX", i)
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

// escapeJSON is a JSON string literal's body, or the literal under quotes.
// A JSON string holds Unicode, so input that is not UTF-8 is refused rather
// than silently rewritten to U+FFFD.
func escapeJSON(s string, quotes bool) (string, error) {
	if !utf8.ValidString(s) {
		return "", errors.New("input is not valid UTF-8, which a JSON string cannot hold")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}
	literal := strings.TrimSuffix(buf.String(), "\n")
	if quotes {
		return literal, nil
	}
	return literal[1 : len(literal)-1], nil
}

func unescapeJSON(s string, quotes bool) (string, error) {
	literal := s
	if !quotes {
		literal = `"` + s + `"`
	}
	var out string
	if err := json.Unmarshal([]byte(literal), &out); err != nil {
		return "", fmt.Errorf("invalid json string: %w", err)
	}
	return out, nil
}

// unescapeShell reads one sh word: single quotes, double quotes (with \", \\,
// \$ and \` escaped) and backslashes, nothing evaluated. Unquoted whitespace
// would make it two words, which is an error.
func unescapeShell(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", errors.New("unterminated single quote")
			}
			b.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case '"':
			i++
			closed := false
			for i < len(s) {
				if s[i] == '"' {
					closed = true
					i++
					break
				}
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return "", errors.New("unterminated double quote")
			}
		case '\\':
			if i+1 >= len(s) {
				return "", errors.New("trailing backslash")
			}
			b.WriteByte(s[i+1])
			i += 2
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return "", errors.New("more than one word: quote the whitespace")
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), nil
}
