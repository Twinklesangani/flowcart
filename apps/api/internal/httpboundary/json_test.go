package httpboundary

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"json", "application/json", `{}`, 200},
		{"charset", "application/json; charset=utf-8", "{} \n", 200},
		{"plain", "text/plain", `{}`, 415},
		{"form", "application/x-www-form-urlencoded", `{}`, 415},
		{"multipart", "multipart/form-data; boundary=test", `{}`, 415},
		{"missing type", "", `{}`, 415},
		{"empty", "application/json", "", 400},
		{"malformed", "application/json", `{`, 400},
		{"garbage", "application/json", `{} garbage`, 400},
		{"second value", "application/json", `{} {}`, 400},
		{"exact limit", "application/json", `{}` + strings.Repeat(" ", int(MutationBodyLimit)-2), 200},
		{"oversized whitespace", "application/json", `{}` + strings.Repeat(" ", int(MutationBodyLimit)), 413},
		{"oversized value", "application/json", `{"name":"` + strings.Repeat("x", int(MutationBodyLimit)) + `"}`, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.ContentLength = -1 // Enforce actual bytes, not a trusted length header.
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			var value map[string]any
			ok := Decode(w, r, &value, MutationBodyLimit)
			if w.Code != tc.status || ok != (tc.status == 200) {
				t.Fatalf("status=%d ok=%v body=%s", w.Code, ok, w.Body)
			}
		})
	}
}
