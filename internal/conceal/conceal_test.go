package conceal

import (
	"bytes"
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
		{"repeated_value", []string{"BCFGW", "BCFGW"}, "BCFGWBCFGW", p + p},
		// The shorter value comes first, so the order of values does not decide.
		{"longest_wins", []string{"abc", "abcdef"}, "abcdef abc abcd", p + " " + p + " " + p + "d"},
		{"empty_value", []string{"", "x"}, "a x", "a " + p},
		{"only_empty_value", []string{""}, "abc", "abc"},
		{"multi_line_value", []string{"l1\nl2"}, "a l1\nl2 l1\n", "a " + p + " l1\n"},
		{"start_of_value_that_does_not_continue", []string{"secret"}, "sector secre", "sector secre"},
		{"start_of_longer_value_at_end", []string{"abcdef", "abc"}, "xabcde", "x" + p + "de"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Writing everything at once and one byte at a time give the same output.
			for _, size := range []int{len(tc.in), 1} {
				var out bytes.Buffer
				w := NewWriter(&out, tc.values)
				for in := tc.in; in != ""; {
					n := min(size, len(in))
					if m, err := w.Write([]byte(in[:n])); m != n || err != nil {
						t.Fatalf("Write(%q) = %d, %v; want %d, nil", in[:n], m, err, n)
					}
					in = in[n:]
				}
				if err := w.Flush(); err != nil {
					t.Fatal(err)
				}
				if out.String() != tc.want {
					t.Errorf("writes of %d bytes: %q; want %q", size, out.String(), tc.want)
				}
			}
		})
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
