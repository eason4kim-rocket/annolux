package searchindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const maxRefreshBatch = 10_000

// RefreshCandidate is an existing document eligible for a conditional GET.
// It deliberately contains no frontier state: maintenance refreshes must not
// discover or lease new URLs.
type RefreshCandidate struct {
	URL         string
	Root        string
	FetchedAt   time.Time
	ContentHash string
	ETag        string
	LastMod     string
}

// ListRefreshCandidates returns the oldest known pages fetched before cutoff.
func (s *Store) ListRefreshCandidates(ctx context.Context, cutoff time.Time, limit int) ([]RefreshCandidate, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	if cutoff.IsZero() {
		return nil, fmt.Errorf("%w: refresh cutoff is required", ErrInvalidConfig)
	}
	if limit <= 0 || limit > maxRefreshBatch {
		return nil, fmt.Errorf("%w: refresh limit must be between 1 and %d", ErrInvalidConfig, maxRefreshBatch)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT url, root, fetched_at, content_hash, etag, last_mod
		FROM pages
		WHERE fetched_at < ?
		ORDER BY fetched_at ASC, url ASC
		LIMIT ?`, cutoff.UTC().Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("searchindex: list refresh candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]RefreshCandidate, 0, limit)
	for rows.Next() {
		var item RefreshCandidate
		var fetchedAt int64
		if err := rows.Scan(&item.URL, &item.Root, &fetchedAt, &item.ContentHash, &item.ETag, &item.LastMod); err != nil {
			return nil, fmt.Errorf("searchindex: scan refresh candidate: %w", err)
		}
		item.FetchedAt = time.Unix(fetchedAt, 0).UTC()
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchindex: read refresh candidates: %w", err)
	}
	return candidates, nil
}

// TouchPage records a successful 304 without rewriting stored content or FTS.
func (s *Store) TouchPage(ctx context.Context, rawURL string, fetchedAt time.Time, etag, lastMod string) error {
	if err := s.guard(ctx); err != nil {
		return err
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || fetchedAt.IsZero() {
		return fmt.Errorf("%w: url and fetched_at are required", ErrInvalidPage)
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	if s.closed {
		return ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result, err := s.db.ExecContext(ctx, `
		UPDATE pages
		SET fetched_at=?, etag=CASE WHEN ?='' THEN etag ELSE ? END,
			last_mod=CASE WHEN ?='' THEN last_mod ELSE ? END
		WHERE url=?`,
		fetchedAt.UTC().Unix(), etag, etag, lastMod, lastMod, rawURL)
	if err != nil {
		return fmt.Errorf("searchindex: touch page: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("searchindex: touch page result: %w", err)
	}
	if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}
