package ratelimit

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefillExpiryAndMemoryBound(t *testing.T) {
	now := time.Unix(100, 0)
	l := New(2, time.Minute, 2, func() time.Time { return now })
	for i := 0; i < 2; i++ {
		if ticket, _ := l.Take("a"); ticket == nil {
			t.Fatal("under limit denied")
		}
	}
	if ticket, retry := l.Take("a"); ticket != nil || retry < 30*time.Second {
		t.Fatal("exhausted budget allowed")
	}
	l.Take("b")
	for i := 0; i < 100; i++ {
		if ticket, _ := l.Take(fmt.Sprint(i)); ticket != nil {
			t.Fatal("full store admitted new key")
		}
	}
	if len(l.entries) != 2 {
		t.Fatal("unbounded entries")
	}
	now = now.Add(30 * time.Second)
	if ticket, _ := l.Take("a"); ticket == nil {
		t.Fatal("refill failed")
	}
	now = now.Add(time.Minute)
	if ticket, _ := l.Take("new"); ticket == nil || len(l.entries) != 1 {
		t.Fatal("expired entries not cleaned")
	}
}

func TestConcurrentBudgetAndRefund(t *testing.T) {
	now := time.Unix(100, 0)
	l := New(5, time.Minute, 20, func() time.Time { return now })
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ticket, _ := l.Take("account"); ticket != nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed=%d", allowed.Load())
	}
	ticket, _ := l.Take("other")
	ticket.Refund()
	ticket.Refund()
	for i := 0; i < 5; i++ {
		if got, _ := l.Take("other"); got == nil {
			t.Fatal("refund failed")
		}
	}
	if got, _ := l.Take("other"); got != nil {
		t.Fatal("double refund added capacity")
	}
}

func TestRemoteAddrIgnoresForwardingHeaders(t *testing.T) {
	l := New(1, time.Minute, 10, nil)
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = fmt.Sprintf("192.0.2.1:%d", 1000+i)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		r.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", i))
		w := httptest.NewRecorder()
		allowed := l.AllowHTTP(w, ClientIP(r))
		if allowed != (i == 0) {
			t.Fatal("forwarding header bypass")
		}
		if !allowed && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("missing 429 contract")
		}
	}
	for _, addr := range []string{"192.0.2.2:1", "[2001:db8::1]:2"} {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = addr
		if ticket, _ := l.Take(ClientIP(r)); ticket == nil {
			t.Fatal("distinct IP denied")
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "[::ffff:192.0.2.1]:9"
	if ticket, _ := l.Take(ClientIP(r)); ticket != nil {
		t.Fatal("mapped IPv4 bypass")
	}
}
