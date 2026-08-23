package publicapi

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type keyedLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type keyLimiters struct {
	mu      sync.Mutex
	entries map[string]*keyedLimiter
	rate    rate.Limit
	burst   int
	now     func() time.Time
}

func newKeyLimiters(requestsPerSecond float64, burst int) *keyLimiters {
	return &keyLimiters{entries: make(map[string]*keyedLimiter), rate: rate.Limit(requestsPerSecond), burst: burst, now: time.Now}
}

func (limiters *keyLimiters) Allow(key string) bool {
	now := limiters.now()
	limiters.mu.Lock()
	defer limiters.mu.Unlock()
	entry := limiters.entries[key]
	if entry == nil {
		entry = &keyedLimiter{limiter: rate.NewLimiter(limiters.rate, limiters.burst)}
		limiters.entries[key] = entry
	}
	entry.lastSeen = now
	if len(limiters.entries) > 10_000 {
		for candidate, value := range limiters.entries {
			if now.Sub(value.lastSeen) > time.Hour {
				delete(limiters.entries, candidate)
			}
		}
	}
	return entry.limiter.AllowN(now, 1)
}
