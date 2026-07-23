// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/jsonx/jsonx_test.go

package jsonx

import "testing"

func mustParse(t *testing.T, s string) any {
	t.Helper()
	v, err := ParseString(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestCompactPreservesKeyOrder(t *testing.T) {
	// Go maps would reorder these alphabetically; jsonx must not.
	v := mustParse(t, `{"z":1,"a":2,"m":3}`)
	if got := Compact(v); got != `{"z":1,"a":2,"m":3}` {
		t.Errorf("Compact = %q", got)
	}
}

func TestCompactNoHTMLEscape(t *testing.T) {
	// encoding/json escapes < > & to < etc; JSON.stringify does not.
	v := mustParse(t, `{"html":"a<b>&\"c\""}`)
	if got := Compact(v); got != `{"html":"a<b>&\"c\""}` {
		t.Errorf("Compact = %q", got)
	}
}

func TestNumberFormatting(t *testing.T) {
	cases := map[float64]string{
		0: "0", 42: "42", -7: "-7", 1000000: "1000000",
		1234.5: "1234.5", 100000000: "100000000",
	}
	for in, want := range cases {
		if got := Number(in); got != want {
			t.Errorf("Number(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestIndent(t *testing.T) {
	v := mustParse(t, `{"a":1,"b":[1,2]}`)
	want := "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ]\n}"
	if got := Indent(v); got != want {
		t.Errorf("Indent =\n%q\nwant\n%q", got, want)
	}
}

func TestIndentEmpty(t *testing.T) {
	if got := Indent(mustParse(t, `{}`)); got != "{}" {
		t.Errorf("empty obj = %q", got)
	}
	if got := Indent(mustParse(t, `[]`)); got != "[]" {
		t.Errorf("empty arr = %q", got)
	}
}
