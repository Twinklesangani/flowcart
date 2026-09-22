package ratelimit

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"flowcart/apps/api/internal/httpboundary"
)

// ClientIP intentionally ignores forwarding headers. No proxy trust is enabled.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	return ip.Unmap().WithZone("").String()
}

func Reject(w http.ResponseWriter, retry time.Duration) {
	seconds := max(1, int(math.Ceil(retry.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.Header().Set("Cache-Control", "no-store")
	httpboundary.Error(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Please try again later.")
}

func (l *Limiter) AllowHTTP(w http.ResponseWriter, key string) bool {
	ticket, retry := l.Take(key)
	if ticket == nil {
		Reject(w, retry)
		return false
	}
	return true
}

// Middleware requires identity to come from authenticated context, not headers.
func (l *Limiter) Middleware(identity func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := identity(r)
			if !ok {
				httpboundary.Error(w, 401, "unauthorized", "Authentication is required.")
				return
			}
			if l.AllowHTTP(w, key) {
				next.ServeHTTP(w, r)
			}
		})
	}
}
