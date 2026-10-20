// Package conceal replaces secret values in a byte stream.
package conceal

import (
	"bytes"
	"io"
	"sort"
)

// Placeholder is written in place of each value.
const Placeholder = "<concealed by kakusu>"

// Writer writes to an underlying writer with every occurrence of its values
// replaced by Placeholder. A value split across Write calls is still
// replaced: Write holds back only the trailing bytes that are the start of a
// value, and Flush writes them when the stream ends.
type Writer struct {
	w      io.Writer
	values [][]byte  // longest first, so the first match at a position is the longest
	first  [256]bool // first bytes of values, so that other bytes are passed on quickly
	held   []byte
}

// NewWriter returns a Writer that conceals each distinct non-empty value in
// values.
func NewWriter(w io.Writer, values []string) *Writer {
	seen := make(map[string]bool)
	var vs [][]byte
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			vs = append(vs, []byte(v))
		}
	}
	sort.Slice(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	cw := &Writer{w: w, values: vs}
	for _, v := range vs {
		cw.first[v[0]] = true
	}
	return cw
}

// Write writes p with values replaced, except for trailing bytes that may be
// the start of a value. It reports len(p) on success, since it has taken the
// bytes it holds.
func (w *Writer) Write(p []byte) (int, error) {
	out, held := w.conceal(append(w.held, p...), false)
	w.held = held
	if len(out) > 0 {
		if _, err := w.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes the bytes held by Write, with complete values replaced and the
// rest as is. Call it after the last Write.
func (w *Writer) Flush() error {
	out, _ := w.conceal(w.held, true)
	w.held = nil
	if len(out) == 0 {
		return nil
	}
	_, err := w.w.Write(out)
	return err
}

// conceal returns buf with values replaced. Unless final, it stops where the
// rest of buf is a proper prefix of a value and returns that rest as held.
func (w *Writer) conceal(buf []byte, final bool) (out, held []byte) {
	for i := 0; i < len(buf); {
		if w.first[buf[i]] {
			if !final && w.startsValue(buf[i:]) {
				return out, buf[i:]
			}
			if n := w.match(buf[i:]); n > 0 {
				out = append(out, Placeholder...)
				i += n
				continue
			}
		}
		out = append(out, buf[i])
		i++
	}
	return out, nil
}

// startsValue reports whether b is a proper prefix of a value, so that the
// bytes after b decide whether, and which, value matches.
func (w *Writer) startsValue(b []byte) bool {
	for _, v := range w.values {
		if len(b) < len(v) && bytes.HasPrefix(v, b) {
			return true
		}
	}
	return false
}

// match returns the length of the longest value at the start of b, or 0.
func (w *Writer) match(b []byte) int {
	for _, v := range w.values {
		if bytes.HasPrefix(b, v) {
			return len(v)
		}
	}
	return 0
}
