package conceal

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

const p = Placeholder

func TestWriterConcealsValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		in     string
		want   string
	}{
		{"value", []string{"QXZVMTHJLN"}, "a QXZVMTHJLN b\n", "a " + p + " b\n"},
		{"repeated_value", []string{"BCFGW", "BCFGW"}, "BCFGW BCFGW", p + " " + p},
		// Overlapping and touching occurrences are one region, so no part of
		// either value is left and one placeholder replaces them.
		{"overlapping_values", []string{"21", "1DUMMYTOKEN"}, "id=21DUMMYTOKEN", "id=" + p},
		{"chain_of_overlaps", []string{"abc", "cde", "efg"}, "xabcdefgx", "x" + p + "x"},
		{"value_inside_value", []string{"abcde", "bc"}, "abcde bc", p + " " + p},
		{"touching_values", []string{"BCFGW"}, "BCFGWBCFGW", p},
		// The shorter value comes first, so the order of values does not decide.
		{"longest_wins", []string{"abc", "abcdef"}, "abcdef abc abcd", p + " " + p + " " + p + "d"},
		{"empty_value", []string{"", "x"}, "a x", "a " + p},
		{"only_empty_value", []string{""}, "abc", "abc"},
		{"multi_line_value", []string{"l1\nl2"}, "a l1\nl2 l1\n", "a " + p + " l1\n"},
		{"start_of_value_that_does_not_continue", []string{"secret"}, "sector secre", "sector secre"},
		{"start_of_longer_value_at_end", []string{"abcdef", "abc"}, "xabcde", "x" + p + "de"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Writing everything at once, one byte at a time, and in two
			// parts split at any point give the same output.
			splits := [][]string{{tc.in}, strings.Split(tc.in, "")}
			for i := 1; i < len(tc.in); i++ {
				splits = append(splits, []string{tc.in[:i], tc.in[i:]})
			}
			for _, writes := range splits {
				if got := concealWrites(t, tc.values, writes); got != tc.want {
					t.Errorf("writes %q: %q; want %q", writes, got, tc.want)
				}
			}
		})
	}
}

// concealWrites writes each of writes to a new Writer for values, flushes
// it, and returns what it wrote.
func concealWrites(t *testing.T, values, writes []string) string {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out, values)
	for _, s := range writes {
		if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", s, n, err, len(s))
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// concealAll is a reference for Writer that sees the whole input at once: it
// marks every byte of every occurrence of every value and replaces each run
// of marked bytes with one placeholder.
func concealAll(values []string, in string) string {
	marked := make([]bool, len(in))
	for _, v := range values {
		if v == "" {
			continue
		}
		for i := 0; i+len(v) <= len(in); i++ {
			if in[i:i+len(v)] == v {
				for j := i; j < i+len(v); j++ {
					marked[j] = true
				}
			}
		}
	}
	var out strings.Builder
	for i := 0; i < len(in); i++ {
		switch {
		case !marked[i]:
			out.WriteByte(in[i])
		case i == 0 || !marked[i-1]:
			out.WriteString(p)
		}
	}
	return out.String()
}

func TestWriterMatchesWholeInputReference(t *testing.T) {
	// A small alphabet makes overlapping and touching occurrences common.
	rng := rand.New(rand.NewSource(1))
	word := func(maxLen int) string {
		b := make([]byte, rng.Intn(maxLen+1))
		for i := range b {
			b[i] = "abc"[rng.Intn(3)]
		}
		return string(b)
	}
	for range 5000 {
		values := make([]string, 1+rng.Intn(3))
		for i := range values {
			values[i] = word(4)
		}
		in := word(24)
		var writes []string
		for rest := in; rest != ""; {
			n := 1 + rng.Intn(len(rest))
			writes = append(writes, rest[:n])
			rest = rest[n:]
		}
		if got, want := concealWrites(t, values, writes), concealAll(values, in); got != want {
			t.Fatalf("values %q, writes %q: %q; want %q", values, writes, got, want)
		}
	}
}

func TestWriterHoldsOnlyStartOfValue(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, []string{"secret"})
	write := func(s, want string) {
		t.Helper()
		w.Write([]byte(s))
		if out.String() != want {
			t.Errorf("after Write(%q): %q; want %q", s, out.String(), want)
		}
	}

	write("ready> ", "ready> ")
	write("sec", "ready> ")
	write("tor\nse", "ready> sector\n")
	write("cret!", "ready> sector\n"+p+"!")
	write(" secr", "ready> sector\n"+p+"! ")

	w.Flush()
	if want := "ready> sector\n" + p + "! secr"; out.String() != want {
		t.Errorf("after Flush: %q; want %q", out.String(), want)
	}
}
