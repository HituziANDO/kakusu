package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveDotenv(t *testing.T) {
	data := Data{
		"a":       {"b": "AB", "b/c": "ABC", "/b": "SLASHB", "b\v": "VT"},
		"a b":     {"c": "SP"},
		"default": {"key": "DEFAULTKEY"},
	}
	const header = "unresolved kks:// references:\n"
	const hint = " (malformed; write kks://group/key or kks://key without whitespace)"

	for _, tc := range []struct {
		name    string
		env     string
		want    map[string]string
		wantErr string
	}{
		{"scheme only", "X=kks://\n", nil, header + "  line 1: X -> kks://" + hint},
		{"empty group", "X=kks:///key\n", nil, header + "  line 1: X -> kks:///key" + hint},
		{"empty key", "X=kks://group/\n", nil, header + "  line 1: X -> kks://group/" + hint},
		{"space in key", "X=kks://a/b c\n", nil, header + "  line 1: X -> kks://a/b c" + hint},
		{"tab in key", "X=kks://a/b\tc\n", nil, header + "  line 1: X -> kks://a/b\tc" + hint},
		{"form feed in key", "X=kks://a/b\fc\n", nil, header + "  line 1: X -> kks://a/b\fc" + hint},
		{"carriage return in key", "X=kks://a/b\rc\n", nil, header + "  line 1: X -> kks://a/b\rc" + hint},
		// \v is not in \s of Go regexp, so the old pattern resolved it too.
		// The quotes keep TrimSpace from removing it.
		{"vertical tab in key", "X=\"kks://a/b\v\"\n", map[string]string{"X": "VT"}, ""},
		{"space in group", "X=kks://a b/c\n", nil, header + "  line 1: X -> kks://a b/c" + hint},
		{"no group with inline comment", "X=kks://openai_key # memo\n", nil, header + "  line 1: X -> kks://openai_key # memo" + hint},
		{"inline comment", "X=kks://a/b # memo\n", nil, header + "  line 1: X -> kks://a/b # memo" + hint},
		{"space inside quotes", "X=\"kks://a/b \"\n", nil, header + "  line 1: X -> kks://a/b " + hint},
		{"no group", "X=kks://key\n", map[string]string{"X": "DEFAULTKEY"}, ""},
		{"no group missing", "X=kks://nokey\n", nil, header + "  line 1: X -> kks://default/nokey"},
		{"slash in key", "X=kks://a/b/c\n", map[string]string{"X": "ABC"}, ""},
		{"key starts with slash", "X=kks://a//b\n", map[string]string{"X": "SLASHB"}, ""},
		{"spaces outside quotes", "X=  kks://a/b  \n", map[string]string{"X": "AB"}, ""},
		{"double quotes", "X=\"kks://a/b\"\n", map[string]string{"X": "AB"}, ""},
		{"single quotes", "X='kks://a/b'\n", map[string]string{"X": "AB"}, ""},
		{"space before scheme inside quotes", "X=\" kks://a/b\"\n", map[string]string{"X": " kks://a/b"}, ""},
		{"uppercase scheme", "X=KKS://a/b\n", map[string]string{"X": "KKS://a/b"}, ""},
		{"single slash after scheme", "X=kks:/a/b\n", map[string]string{"X": "kks:/a/b"}, ""},
		{"one-sided quote", "X=\"kks://a/b\n", map[string]string{"X": "\"kks://a/b"}, ""},
		{
			"malformed and unresolved in line order",
			"# comment\n" +
				"A=kks://a/missing\n" +
				"B=kks://\n" +
				"\n" +
				"C=plain\n" +
				"D=kks://nokey\n" +
				"E=kks://a/b c\n" +
				"F=kks://a/b\n",
			nil,
			header +
				"  line 2: A -> kks://a/missing\n" +
				"  line 3: B -> kks://" + hint + "\n" +
				"  line 6: D -> kks://default/nokey\n" +
				"  line 7: E -> kks://a/b c" + hint,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tc.env), 0600); err != nil {
				t.Fatal(err)
			}
			got, _, err := ResolveDotenv(path, data)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr || got != nil {
					t.Errorf("got %q, error %v; want error %q", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, error %v; want %q", got, err, tc.want)
			}
		})
	}
}
