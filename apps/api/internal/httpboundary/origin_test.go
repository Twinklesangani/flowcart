package httpboundary

import "testing"

func TestExactOriginPolicy(t *testing.T) {
	p, err := NewOriginPolicy("https://APP.example.com:443/,https://app.example.com:8443,http://[::1]:3000", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"https://app.example.com", "https://app.example.com:443", "https://app.example.com:8443", "http://[::1]:3000"} {
		if !p.TrustedOrigin(v) {
			t.Fatalf("trusted origin rejected: %s", v)
		}
	}
	for _, v := range []string{"https://evil-app.example.com", "https://app.example.com.evil.test", "http://app.example.com", "https://app.example.com:8444", "null", "", "https://user@app.example.com", "https://app.example.com/path"} {
		if p.TrustedOrigin(v) {
			t.Fatalf("untrusted accepted: %s", v)
		}
	}
	for _, v := range []string{"*", "https://*.example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com#x", "https://u:p@example.com", "https://example.com:99999", "https://example.com,", "null"} {
		if _, err := NewOriginPolicy(v, false); err == nil {
			t.Fatalf("invalid configuration accepted: %s", v)
		}
	}
}
