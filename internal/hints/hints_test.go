// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/hints/hints_test.go

package hints

import (
	"reflect"
	"strings"
	"testing"
)

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func TestEmptyAndWhitespace(t *testing.T) {
	if got := Extract(""); len(got) != 0 {
		t.Errorf("empty → %v", got)
	}
	if got := Extract("   \n\t "); len(got) != 0 {
		t.Errorf("whitespace → %v", got)
	}
}

func TestMostFrequentFirst(t *testing.T) {
	if got := Extract("checkout checkout checkout page render render done"); got[0] != "checkout" {
		t.Errorf("first = %q", got[0])
	}
}

func TestExcludesStopwords(t *testing.T) {
	h := Extract("the the the and and with this that from checkout")
	if contains(h, "the") || contains(h, "and") || !contains(h, "checkout") {
		t.Errorf("stopwords: %v", h)
	}
}

func TestBoostsIdentifierAndSnake(t *testing.T) {
	if got := Extract("orders sessionToken"); got[0] != "sessionToken" {
		t.Errorf("camel boost: %v", got)
	}
	if got := Extract("orders session_token"); got[0] != "session_token" {
		t.Errorf("snake boost: %v", got)
	}
}

func TestBoostsProperNoun(t *testing.T) {
	if got := Extract("orders Github"); got[0] != "Github" {
		t.Errorf("proper boost: %v", got)
	}
}

func TestDedupCaseInsensitiveFirstCasing(t *testing.T) {
	h := Extract("Checkout checkout CHECKOUT orders")
	n := 0
	for _, x := range h {
		if strings.ToLower(x) == "checkout" {
			n++
		}
	}
	if n != 1 || !contains(h, "Checkout") {
		t.Errorf("dedup/casing: %v", h)
	}
}

func TestCapAndCustomMax(t *testing.T) {
	if got := Extract("alpha bravo charlie delta echo foxtrot golf hotel india"); len(got) > 5 {
		t.Errorf("cap 5: %d", len(got))
	}
	if got := ExtractN("alpha bravo charlie delta echo foxtrot", 3); len(got) != 3 {
		t.Errorf("custom max: %d", len(got))
	}
	if got := ExtractN("checkout orders", 0); len(got) != 0 {
		t.Errorf("max 0: %v", got)
	}
	if got := ExtractN("checkout orders", -1); len(got) != 0 {
		t.Errorf("max -1: %v", got)
	}
}

func TestIgnoresShortAndNumbers(t *testing.T) {
	h := Extract("ab a 42 402 checkout")
	if contains(h, "ab") || contains(h, "42") || contains(h, "402") || !contains(h, "checkout") {
		t.Errorf("short/numbers: %v", h)
	}
}

func TestSkipsLongBlobs(t *testing.T) {
	blob := strings.Repeat("x", 60)
	if contains(Extract(blob+" checkout"), blob) {
		t.Errorf("long blob not skipped")
	}
}

func TestDeterministicAndAlphabeticalTiebreak(t *testing.T) {
	c := "playwright snapshot button button form input sessionId sessionId"
	if !reflect.DeepEqual(Extract(c), Extract(c)) {
		t.Errorf("non-deterministic")
	}
	if got := ExtractN("delta charlie bravo alpha", 2); !reflect.DeepEqual(got, []string{"alpha", "bravo"}) {
		t.Errorf("alphabetical tiebreak: %v", got)
	}
}
