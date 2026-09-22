package ratelimit

import (
	"crypto/sha256"
	"sync"
	"time"
)

const DefaultMaxEntries = 4096

type bucket struct {
	tokens           float64
	updated, expires time.Time
}

// Limiter is a bounded token bucket store. Idle entries expire after one full
// refill period. Cleanup runs on traffic, at most once per second; no goroutine
// or shutdown hook is needed. Full stores reject new keys rather than evicting
// active budgets (which would let key churn bypass limits).
type Limiter struct {
	mu          sync.Mutex
	entries     map[[32]byte]*bucket
	capacity    int
	period      time.Duration
	maxEntries  int
	now         func() time.Time
	nextCleanup time.Time
}

func New(capacity int, period time.Duration, maxEntries int, now func() time.Time) *Limiter {
	if capacity <= 0 || period <= 0 || maxEntries <= 0 {
		panic("invalid rate limiter configuration")
	}
	if now == nil {
		now = time.Now
	}
	return &Limiter{entries: make(map[[32]byte]*bucket), capacity: capacity, period: period, maxEntries: maxEntries, now: now}
}

// Ticket reserves capacity before work starts, so concurrent attempts cannot
// bypass a failure budget. Refund only when the attempt should not count.
type Ticket struct {
	once    sync.Once
	limiter *Limiter
	key     [32]byte
	bucket  *bucket
}

func (l *Limiter) Take(identity string) (*Ticket, time.Duration) {
	key := sha256.Sum256([]byte(identity))
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextCleanup) {
		for key, b := range l.entries {
			if !now.Before(b.expires) {
				delete(l.entries, key)
			}
		}
		l.nextCleanup = now.Add(time.Second)
	}
	b := l.entries[key]
	if b != nil && !now.Before(b.expires) {
		delete(l.entries, key)
		b = nil
	}
	if b == nil {
		if len(l.entries) >= l.maxEntries {
			return nil, time.Second
		}
		b = &bucket{tokens: float64(l.capacity), updated: now}
		l.entries[key] = b
	}
	l.refill(b, now)
	if b.tokens < 1 {
		return nil, time.Duration((1-b.tokens)*float64(l.period)/float64(l.capacity)) + time.Nanosecond
	}
	b.tokens--
	b.expires = now.Add(l.period)
	return &Ticket{limiter: l, key: key, bucket: b}, 0
}

func (l *Limiter) refill(b *bucket, now time.Time) {
	if now.After(b.updated) {
		b.tokens = min(float64(l.capacity), b.tokens+now.Sub(b.updated).Seconds()*float64(l.capacity)/l.period.Seconds())
		b.updated = now
	}
}

func (t *Ticket) Refund() {
	t.once.Do(func() {
		l := t.limiter
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.entries[t.key] == t.bucket {
			l.refill(t.bucket, l.now())
			t.bucket.tokens = min(float64(l.capacity), t.bucket.tokens+1)
		}
	})
}
