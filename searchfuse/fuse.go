// Package searchfuse merges the full-text core lane with the discovery lane
// by reciprocal-rank fusion. It lives outside the digest-frozen ranking
// files: the core lane is the frozen Store.Query called verbatim, the
// discovery lane is the lite index's column-weighted bm25, and fusion sees
// only their ranked outputs. BM25 statistics differ between the two indexes,
// so scores are never compared — ranks are.
//
// Disabled, the fuser is a transparent passthrough to the frozen query, and
// a single-lane fusion preserves that lane's order exactly (1/(k+rank) is
// strictly decreasing), so enabling the flag over an empty discovery tier
// changes nothing at all. That equivalence is what lets the code ship dark.
package searchfuse

import (
	"context"
	"sort"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

const (
	// DefaultRRFK is the standard reciprocal-rank constant; DefaultWindow is
	// how deep each lane is consulted. Both are tuning surface for the v3
	// dev split and nothing else.
	DefaultRRFK   = 60
	DefaultWindow = 40
)

// Config controls one fuser. The zero value is disabled passthrough.
type Config struct {
	Enabled bool
	RRFK    int
	Window  int
}

// Stats reports what fusion actually did for one query, for the query log.
type Stats struct {
	Fused      bool
	Tier1Hits  int // discovery-lane candidates consulted
	Tier1Added int // results served from the discovery lane alone
}

// Fuser owns the two-lane query path over one store.
type Fuser struct {
	store  *searchindex.Store
	config Config
}

func New(store *searchindex.Store, config Config) *Fuser {
	if config.RRFK <= 0 {
		config.RRFK = DefaultRRFK
	}
	if config.Window <= 0 {
		config.Window = DefaultWindow
	}
	return &Fuser{store: store, config: config}
}

// Query returns the fused candidate list. Disabled, it is exactly the frozen
// query with the caller's limit.
func (f *Fuser) Query(ctx context.Context, text string, limit int) ([]searchindex.Hit, Stats, error) {
	if !f.config.Enabled {
		hits, err := f.store.Query(ctx, text, limit)
		return hits, Stats{}, err
	}
	// The frozen query's internal rerank window is fixed and independent of
	// the limit, so consulting it deeper only extends the list; the head
	// stays what a smaller limit would have returned.
	coreWindow := max(limit, f.config.Window)
	core, err := f.store.Query(ctx, text, coreWindow)
	if err != nil {
		return nil, Stats{}, err
	}
	discovery, err := f.discoveryLane(ctx, text, f.config.Window)
	if err != nil {
		return nil, Stats{}, err
	}
	stats := Stats{Fused: true, Tier1Hits: len(discovery)}
	if len(discovery) == 0 {
		if len(core) > limit {
			core = core[:limit]
		}
		return core, stats, nil
	}
	fused := fuse(core, discovery, f.config.RRFK, limit)
	for _, hit := range fused {
		if hit.fromDiscoveryOnly {
			stats.Tier1Added++
		}
	}
	hits := make([]searchindex.Hit, 0, len(fused))
	for _, hit := range fused {
		hits = append(hits, hit.hit)
	}
	return hits, stats, nil
}

// discoveryLane queries the lite tier in both languages, serving the detected
// query language in full before any cross-language hits. Interleaving the two
// (the first design) quietly halved every preferred-language rank — an en
// target at lane rank 4 fused as rank 7-8 and fell out of the top ten, which
// the v3 dev split exposed as eleven "found by the lane, lost in fusion"
// misses. RRF ranks are the lane's only voice, so its order must not be
// diluted by a language the query didn't ask for; cross-language serendipity
// still enters, just after the preferred hits.
//
// Promoted rows are included deliberately: a page whose body sits in the core
// is usually served by the core lane, and when both lanes agree RRF's summed
// contribution boosts it — the consensus effect fusion exists for. When the
// core misses a promoted page entirely (broad head queries do this), the
// discovery lane is its only road back into the results; excluding promoted
// rows made those pages unreachable from either lane.
func (f *Fuser) discoveryLane(ctx context.Context, text string, window int) ([]searchindex.LiteHit, error) {
	preferred := searchindex.DetectLanguage(text)
	other := searchindex.LangEnglish
	if preferred == searchindex.LangEnglish {
		other = searchindex.LangChinese
	}
	preferredHits, err := f.store.QueryLite(ctx, preferred, text, window, true)
	if err != nil {
		return nil, err
	}
	otherHits, err := f.store.QueryLite(ctx, other, text, window, true)
	if err != nil {
		return nil, err
	}
	merged := make([]searchindex.LiteHit, 0, window)
	seen := make(map[string]struct{}, window)
	for _, hit := range preferredHits {
		if len(merged) >= window {
			return merged, nil
		}
		if _, exists := seen[hit.URL]; exists {
			continue
		}
		seen[hit.URL] = struct{}{}
		merged = append(merged, hit)
	}
	for _, hit := range otherHits {
		if len(merged) >= window {
			return merged, nil
		}
		if _, exists := seen[hit.URL]; exists {
			continue
		}
		seen[hit.URL] = struct{}{}
		merged = append(merged, hit)
	}
	return merged, nil
}

type fusedHit struct {
	hit               searchindex.Hit
	score             float64
	inCore            bool
	fromDiscoveryOnly bool
}

// fuse assigns every URL the sum of its reciprocal ranks across the lanes it
// appears in and orders by that sum. Ties break toward the core lane — a
// page with a stored body serves a better answer than a sketch — and then by
// URL for determinism.
func fuse(core []searchindex.Hit, discovery []searchindex.LiteHit, k, limit int) []fusedHit {
	byURL := make(map[string]*fusedHit, len(core)+len(discovery))
	order := make([]string, 0, len(core)+len(discovery))
	for rank, hit := range core {
		entry := &fusedHit{hit: hit, inCore: true, score: 1.0 / float64(k+rank+1)}
		byURL[hit.URL] = entry
		order = append(order, hit.URL)
	}
	for rank, lite := range discovery {
		contribution := 1.0 / float64(k+rank+1)
		if existing, ok := byURL[lite.URL]; ok {
			existing.score += contribution
			continue
		}
		byURL[lite.URL] = &fusedHit{
			hit: searchindex.Hit{
				URL: lite.URL, Title: lite.Title, Root: lite.Root,
				FetchedAt: lite.FetchedAt, Score: lite.Score,
				Snippet: lite.Summary, Lang: lite.Lang,
			},
			score: contribution, fromDiscoveryOnly: true,
		}
		order = append(order, lite.URL)
	}
	ranked := make([]fusedHit, 0, len(order))
	for _, url := range order {
		ranked = append(ranked, *byURL[url])
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].inCore != ranked[j].inCore {
			return ranked[i].inCore
		}
		return ranked[i].hit.URL < ranked[j].hit.URL
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}
