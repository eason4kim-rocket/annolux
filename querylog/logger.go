package querylog

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	queueCapacity  = 1024
	flushBatchSize = 128
	flushInterval  = 500 * time.Millisecond
	expireInterval = time.Hour
	// errorLogInterval rate-limits write-failure logging: a read-only disk
	// would otherwise emit one error line per dropped batch forever.
	errorLogInterval = time.Minute
)

// Logger is the asynchronous, bounded front of the store. Log never blocks
// and never returns an error: when the queue is full or the disk is failing,
// records are counted as dropped and the Search path proceeds untouched.
type Logger struct {
	store     *Store
	logger    *slog.Logger
	retention time.Duration
	queue     chan Record
	dropped   atomic.Int64
	closeOnce sync.Once
	closed    chan struct{}
	done      chan struct{}
	lastError atomic.Int64
}

// NewLogger starts the writer goroutine over an open store. The logger owns
// the store from here on and closes it on Close.
func NewLogger(store *Store, logger *slog.Logger, retention time.Duration) *Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if retention <= 0 {
		retention = DefaultRetention
	}
	l := &Logger{
		store: store, logger: logger, retention: retention,
		queue: make(chan Record, queueCapacity), closed: make(chan struct{}), done: make(chan struct{}),
	}
	go l.run()
	return l
}

// Log enqueues one record without blocking. After Close it is a no-op. The
// queue channel is never closed, so a Log racing Close can never panic; a
// record that slips in after shutdown is simply never read.
func (l *Logger) Log(record Record) {
	select {
	case <-l.closed:
		return
	default:
	}
	select {
	case l.queue <- record:
	default:
		l.dropped.Add(1)
	}
}

// Dropped reports how many records were lost to backpressure or write errors.
func (l *Logger) Dropped() int64 { return l.dropped.Load() }

// Flush blocks until every record enqueued before the call is written or
// dropped. It exists for tests and shutdown, not for the request path.
func (l *Logger) Flush(ctx context.Context) error {
	for {
		if len(l.queue) == 0 {
			// One empty poll is not proof the writer finished its batch, so
			// wait one flush interval past empty before declaring done.
			timer := time.NewTimer(flushInterval + 50*time.Millisecond)
			select {
			case <-timer.C:
				if len(l.queue) == 0 {
					return nil
				}
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Close stops intake, writes what is queued, and closes the store.
func (l *Logger) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	<-l.done
	return l.store.Close()
}

func (l *Logger) run() {
	defer close(l.done)
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()
	expireTicker := time.NewTicker(expireInterval)
	defer expireTicker.Stop()
	batch := make([]Record, 0, flushBatchSize)
	l.expire()
	for {
		select {
		case record := <-l.queue:
			batch = append(batch, record)
			if len(batch) >= flushBatchSize {
				l.write(batch)
				batch = batch[:0]
			}
		case <-flushTicker.C:
			if len(batch) > 0 {
				l.write(batch)
				batch = batch[:0]
			}
		case <-expireTicker.C:
			l.expire()
		case <-l.closed:
			for {
				select {
				case record := <-l.queue:
					batch = append(batch, record)
					if len(batch) >= flushBatchSize {
						l.write(batch)
						batch = batch[:0]
					}
				default:
					l.write(batch)
					return
				}
			}
		}
	}
}

func (l *Logger) write(batch []Record) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.store.InsertBatch(ctx, batch); err != nil {
		l.dropped.Add(int64(len(batch)))
		l.reportError("query log write failed", err, len(batch))
	}
}

func (l *Logger) expire() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := l.store.ExpireQueryText(ctx, time.Now().Add(-l.retention)); err != nil {
		l.reportError("query log expiry failed", err, 0)
	}
}

func (l *Logger) reportError(message string, err error, batchSize int) {
	now := time.Now().Unix()
	last := l.lastError.Load()
	if now-last < int64(errorLogInterval/time.Second) || !l.lastError.CompareAndSwap(last, now) {
		return
	}
	l.logger.Error(message, "error", err, "batch", batchSize, "dropped_total", l.dropped.Load())
}
