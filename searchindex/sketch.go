package searchindex

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	// sketchMaxTerms bounds one page's sketch; sketchHiTerms is the slice of
	// it that lands in the higher-weighted FTS column. The split lets two
	// detail=column weights approximate a significance gradient without
	// storing positions or scores.
	sketchMaxTerms = 128
	sketchHiTerms  = 32

	// sketchMinTermRunes drops one-rune Latin fragments; single Han runes
	// stay because the segmenter deliberately emits meaningful single
	// characters.
	sketchMinTermRunes = 2
)

// DocumentFrequencies answers how many corpus documents contain a term, and
// how many documents the corpus holds, for one language. The sketcher only
// reads; building the table is the backfill's job.
type DocumentFrequencies interface {
	DocumentFrequency(term string) int64
	Documents() int64
}

// Sketch is one page's lexical fingerprint: the body's most significant
// unique terms, split so the discovery index can weight the top slice above
// the tail.
type Sketch struct {
	SalientHi string
	SalientLo string
}

// BuildSketch reduces a body to the terms most likely to matter in a query
// that this page should answer: unique body terms ranked by tf·idf against
// the corpus, with terms the title or headings already index excluded — they
// would be counted twice otherwise. The result is deterministic for a given
// body and frequency table.
func BuildSketch(lang, title, headings, body string, frequencies DocumentFrequencies) (Sketch, error) {
	bodyTokens, err := sketchTokens(lang, body)
	if err != nil {
		return Sketch{}, err
	}
	if len(bodyTokens) == 0 {
		return Sketch{}, nil
	}
	excluded := map[string]struct{}{}
	for _, source := range []string{title, headings} {
		tokens, err := sketchTokens(lang, source)
		if err != nil {
			return Sketch{}, err
		}
		for _, token := range tokens {
			excluded[token] = struct{}{}
		}
	}

	counts := make(map[string]int, len(bodyTokens))
	for _, token := range bodyTokens {
		if _, skip := excluded[token]; skip {
			continue
		}
		counts[token]++
	}
	if len(counts) == 0 {
		return Sketch{}, nil
	}

	documents := float64(frequencies.Documents())
	if documents < 1 {
		documents = 1
	}
	type scored struct {
		term  string
		score float64
	}
	ranked := make([]scored, 0, len(counts))
	for term, count := range counts {
		frequency := float64(frequencies.DocumentFrequency(term))
		significance := float64(count) * math.Log((documents+1)/(frequency+1))
		ranked = append(ranked, scored{term: term, score: significance})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].term < ranked[j].term
	})
	if len(ranked) > sketchMaxTerms {
		ranked = ranked[:sketchMaxTerms]
	}
	split := min(sketchHiTerms, len(ranked))
	hi := make([]string, 0, split)
	lo := make([]string, 0, len(ranked)-split)
	for index, item := range ranked {
		if index < split {
			hi = append(hi, item.term)
		} else {
			lo = append(lo, item.term)
		}
	}
	return Sketch{SalientHi: strings.Join(hi, " "), SalientLo: strings.Join(lo, " ")}, nil
}

// sketchTokens produces the normalized terms significance is computed over.
// The text first passes through the same rewrite the FTS index applies —
// Chinese is segmented, English passes through — so sketch terms and index
// terms can never disagree about what a word is. unicode61's separator
// behavior is approximated by splitting on anything that is neither letter
// nor digit.
func sketchTokens(lang, text string) ([]string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	rewritten, err := indexText(lang, text)
	if err != nil {
		return nil, err
	}
	fields := strings.FieldsFunc(rewritten, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.ToLower(field)
		runes := []rune(token)
		if len(runes) < sketchMinTermRunes && !isHan(runes) {
			continue
		}
		if isDigits(token) {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func isHan(runes []rune) bool {
	for _, r := range runes {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return len(runes) > 0
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
