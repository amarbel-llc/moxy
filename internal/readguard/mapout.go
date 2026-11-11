package readguard

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// MapOutput copies r to w, writing orig (the operand as the caller named it)
// wherever the tool printed repl, the stand-in readguard gave it:
//
//   - a "/dev/fd/N" repl is replaced wherever it appears;
//   - a "./" repl stands for an entered directory ".", so a line that is ".",
//     starts ".:" (ls headers, file(1)), starts "./", has "\t." or "\t./"
//     (du), or holds the JSON "name":"." (tree -J) is rewritten.
func MapOutput(w io.Writer, r io.Reader, orig, repl string) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			if _, werr := io.WriteString(w, mapLine(line, orig, repl)); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func mapLine(line, orig, repl string) string {
	if repl != "./" {
		return strings.ReplaceAll(line, repl, orig)
	}
	dir := strings.TrimSuffix(orig, "/")
	if orig == "/" {
		dir = ""
	}
	body, nl := strings.CutSuffix(line, "\n")
	nlText := ""
	if nl {
		nlText = "\n"
	}
	switch {
	case body == ".":
		body = orig
	case strings.HasPrefix(body, ".:"):
		body = orig + body[1:]
	case strings.HasPrefix(body, "./"):
		body = dir + body[1:]
	}
	if i := strings.Index(body, "\t."); i >= 0 {
		rest := body[i+2:]
		switch {
		case rest == "":
			body = body[:i+1] + orig
		case strings.HasPrefix(rest, "/"):
			body = body[:i+1] + dir + rest
		}
	}
	if strings.Contains(body, `"name":"."`) {
		quoted, _ := json.Marshal(orig)
		body = strings.ReplaceAll(body, `"name":"."`, `"name":`+string(quoted))
	}
	return body + nlText
}
