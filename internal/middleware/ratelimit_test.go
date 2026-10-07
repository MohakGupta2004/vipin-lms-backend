package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterBlocksAfterLimitAndRecovers(t *testing.T) {
	rl := NewRateLimiter(context.Background(), 2, time.Minute, false)
	now := time.Now()
	for i := 0; i < 2; i++ {
		if _, ok := rl.allow("1.1.1.1", now); !ok {
			t.Fatalf("request %d blocked", i)
		}
	}
	if retry, ok := rl.allow("1.1.1.1", now); ok || retry <= 0 {
		t.Fatalf("third request allowed or no retry hint: ok=%v retry=%v", ok, retry)
	}
	if _, ok := rl.allow("2.2.2.2", now); !ok {
		t.Error("other client must not be affected")
	}
	if _, ok := rl.allow("1.1.1.1", now.Add(time.Minute+time.Second)); !ok {
		t.Error("window should have expired")
	}
}

func TestRateLimiterMiddleware429(t *testing.T) {
	rl := NewRateLimiter(context.Background(), 1, time.Minute, false)
	h := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	codes := []int{}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
		if rec.Code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Error("missing Retry-After")
		}
	}
	if codes[0] != 200 || codes[1] != 429 {
		t.Errorf("codes = %v", codes)
	}
}

func TestClientIPProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:80"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 5.5.5.5")
	if got := NewRateLimiter(context.Background(), 1, time.Minute, true).clientIP(req); got != "5.5.5.5" {
		t.Errorf("trusted: %s", got)
	}
	if got := NewRateLimiter(context.Background(), 1, time.Minute, false).clientIP(req); got != "10.0.0.1" {
		t.Errorf("untrusted: %s", got)
	}
}
