package httpboundary

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// OriginPolicy is immutable after construction and shared by CORS and auth.
// Its zero value denies every browser origin.
type OriginPolicy struct{ allowed map[string]bool }

func NewOriginPolicy(value string, production bool) (OriginPolicy, error) {
	if strings.TrimSpace(value) == "" {
		if production {
			return OriginPolicy{}, errors.New("CORS_ALLOWED_ORIGINS is required in production")
		}
		value = "http://localhost:3000,http://localhost:3001"
	}
	p := OriginPolicy{allowed: make(map[string]bool)}
	for _, raw := range strings.Split(value, ",") {
		origin, err := canonicalOrigin(strings.TrimSpace(raw))
		if err != nil || (production && !strings.HasPrefix(origin, "https://")) {
			return OriginPolicy{}, errors.New("CORS_ALLOWED_ORIGINS must contain exact HTTP(S) origins without wildcards, credentials, paths, queries or fragments; production requires HTTPS")
		}
		p.allowed[origin] = true
	}
	return p, nil
}

func canonicalOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || strings.ContainsAny(value, "*\\\t\r\n ") {
		return "", errors.New("invalid origin")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
		return "", errors.New("invalid origin")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "%") {
		return "", errors.New("invalid host")
	}
	for _, c := range host {
		if c > 127 {
			return "", errors.New("use ASCII origin host")
		}
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("invalid port")
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid port")
		}
		port = strconv.Itoa(n)
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

func (p OriginPolicy) TrustedOrigin(origin string) bool {
	canonical, err := canonicalOrigin(origin)
	return err == nil && p.allowed[canonical]
}

func (p OriginPolicy) AllowAuthMutation(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 || !p.TrustedOrigin(origins[0]) {
		Error(w, http.StatusForbidden, "untrusted_origin", "Request origin is not allowed.")
		return false
	}
	return true
}
