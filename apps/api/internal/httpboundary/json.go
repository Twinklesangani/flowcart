package httpboundary

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const AuthBodyLimit int64 = 16 * 1024
const MutationBodyLimit int64 = 64 * 1024

// Decode bounds the entire body before parsing or invoking domain services.
// Reading the bounded body first also catches oversized trailing whitespace.
func Decode(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		Error(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			Error(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body is too large.")
		} else {
			Error(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
		}
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if decoder.Decode(target) != nil {
		Error(w, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON.")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		Error(w, http.StatusBadRequest, "invalid_request", "Request body must contain one JSON document.")
		return false
	}
	return true
}

func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
