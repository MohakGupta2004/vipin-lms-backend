package middleware

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
)

const maxTrackedClients = 100_000

// RateLimiter allows at most limit requests per window for each client IP (sliding window).
// State is in memory, so limits are per instance. uber-go/ratelimit is not used here:
// it delays callers to a steady rate, while an API must reject excess requests per client.
type RateLimiter struct {
	limit      int
	window     time.Duration
	trustProxy bool

	mu   sync.Mutex
	hits map[string][]time.Time
}

// NewRateLimiter starts the limiter; its cleanup goroutine stops when ctx is cancelled.
// With trustProxy the client IP comes from the last X-Forwarded-For entry (the one the nearest proxy appended).
func NewRateLimiter(ctx context.Context, limit int, window time.Duration, trustProxy bool) *RateLimiter {
	rl := &RateLimiter{limit: limit, window: window, trustProxy: trustProxy, hits: make(map[string][]time.Time)}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				rl.sweep(now)
			}
		}
	}()
	return rl
}

// Limit rejects requests over the limit with 429 and a Retry-After header.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if retry, ok := rl.allow(rl.clientIP(r), time.Now()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			utils.WriteJSONResponse(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(key string, now time.Time) (retryAfter time.Duration, ok bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := now.Add(-rl.window)
	recent := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= rl.limit {
		rl.hits[key] = recent
		return recent[0].Add(rl.window).Sub(now), false
	}
	if _, tracked := rl.hits[key]; !tracked && len(rl.hits) >= maxTrackedClients {
		return rl.window, false // fail closed rather than grow without bound
	}
	rl.hits[key] = append(recent, now)
	return 0, true
}

func (rl *RateLimiter) sweep(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.window)
	for key, ts := range rl.hits {
		if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
			delete(rl.hits, key)
		}
	}
}

func (rl *RateLimiter) clientIP(r *http.Request) string {
	if rl.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
