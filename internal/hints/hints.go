// Package hints extracts a few salient search terms from stored tool output,
// emitted in the recall header so Claude's first recall__search lands on a real
// term. Deterministic frequency-and-shape heuristics — no LLM, no network.
// Ports src/hints.ts.
package hints

import (
	"regexp"
	"sort"
	"strings"
)

const (
	defaultMaxHints = 5
	minTokenLen     = 3
	maxTokenLen     = 40
	identifierBoost = 2
	properNounBoost = 1
)

// tokenRe: tokens start with a letter; digits/underscore allowed so identifiers survive.
var tokenRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*`)

// camelRe: a camelCase / PascalCase boundary, e.g. the "nT" in "sessionToken".
var camelRe = regexp.MustCompile(`[a-z][A-Z]`)

var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "with": true, "this": true, "that": true, "from": true, "into": true, "over": true,
	"has": true, "have": true, "had": true, "was": true, "were": true, "will": true, "would": true, "should": true, "could": true,
	"not": true, "but": true, "you": true, "your": true, "yours": true, "all": true, "any": true, "can": true, "use": true, "used": true,
	"using": true, "its": true, "out": true, "per": true, "via": true, "one": true, "two": true, "get": true, "set": true, "new": true,
	"null": true, "true": true, "false": true, "none": true, "nan": true, "undefined": true,
}

func isIdentifier(token string) bool {
	return strings.Contains(token, "_") || camelRe.MatchString(token)
}

type tokenAcc struct {
	display    string
	count      int
	identifier bool
	proper     bool
}

// Extract returns up to defaultMaxHints salient terms from content.
func Extract(content string) []string { return ExtractN(content, defaultMaxHints) }

// ExtractN returns up to maxHints salient terms, ranked by frequency with a
// boost for identifier-shaped and capitalized tokens. Deterministic:
// equal-scoring tokens are ordered alphabetically by lowercased form.
func ExtractN(content string, maxHints int) []string {
	if maxHints <= 0 {
		return []string{}
	}
	acc := map[string]*tokenAcc{}
	var order []string
	for _, raw := range tokenRe.FindAllString(content, -1) {
		if len(raw) < minTokenLen || len(raw) > maxTokenLen {
			continue
		}
		key := strings.ToLower(raw)
		if stopwords[key] {
			continue
		}
		if existing := acc[key]; existing != nil {
			existing.count++
		} else {
			acc[key] = &tokenAcc{
				display:    raw,
				count:      1,
				identifier: isIdentifier(raw),
				proper:     raw[0] >= 'A' && raw[0] <= 'Z',
			}
			order = append(order, key)
		}
	}

	type ranked struct {
		display string
		key     string
		score   int
	}
	scored := make([]ranked, 0, len(order))
	for _, key := range order {
		t := acc[key]
		score := t.count
		if t.identifier {
			score += identifierBoost
		}
		if t.proper {
			score += properNounBoost
		}
		scored = append(scored, ranked{t.display, strings.ToLower(t.display), score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].key < scored[j].key
	})

	n := len(scored)
	if n > maxHints {
		n = maxHints
	}
	out := make([]string, 0, n)
	for _, r := range scored[:n] {
		out = append(out, r.display)
	}
	return out
}
