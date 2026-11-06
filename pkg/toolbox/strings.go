package toolbox

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

// defaultStringsMin is GNU strings' default minimum run length.
const defaultStringsMin = 4

func positive(v string) error {
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n == 0 {
		return fmt.Errorf("want a positive integer, got %q", v)
	}
	return nil
}

// stringsApplet is GNU strings(1) for plain bytes (moxy#184): each run of at
// least -n printable ASCII characters (tab included), one per line.
var stringsApplet = &Applet{
	Name:    "strings",
	Summary: "printable character runs in binary inputs",
	Flags: []Flag{
		{Short: 'n', Long: "bytes", Value: true, Valid: positive},
	},
	FileOperands: true,
	MaxOperands:  -1,
}

func printableByte(b byte) bool { return b == '\t' || (b >= 0x20 && b < 0x7f) }

// printableRuns writes every run of at least minLen printable bytes of each
// input, one per line. Memory is bounded by minLen: a run is held only until
// it is long enough to print, then streamed.
func printableRuns(minLen int, inputs []namedReader, out io.Writer) error {
	w := bufio.NewWriter(out)
	pending := make([]byte, 0, minLen)
	for _, in := range inputs {
		r := bufio.NewReader(in.r)
		streaming := false
		end := func() error {
			if streaming {
				if err := w.WriteByte('\n'); err != nil {
					return err
				}
			}
			pending, streaming = pending[:0], false
			return nil
		}
		for {
			b, err := r.ReadByte()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("strings: %s: %w", in.name, err)
			}
			switch {
			case !printableByte(b):
				if err := end(); err != nil {
					return err
				}
			case streaming:
				if err := w.WriteByte(b); err != nil {
					return err
				}
			default:
				pending = append(pending, b)
				if len(pending) == minLen {
					if _, err := w.Write(pending); err != nil {
						return err
					}
					streaming = true
				}
			}
		}
		if err := end(); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runStrings(st Stage, inputs []namedReader, out io.Writer) error {
	minLen := defaultStringsMin
	if v := st.Flags["bytes"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		minLen = n
	}
	return printableRuns(minLen, inputs, out)
}
