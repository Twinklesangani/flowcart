package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func TestRequestLoggerEmitsSafeBoundedFields(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(requestLogger)
	router.Get("/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	})
	request := httptest.NewRequest(http.MethodGet, "/health?token=must-not-log", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	logged := output.String()
	for _, field := range []string{"request_id", "method", "route", "status", "duration_ms", "error_code"} {
		if !strings.Contains(logged, `"`+field+`"`) {
			t.Fatalf("log missing field %q: %s", field, logged)
		}
	}
	if strings.Contains(logged, "must-not-log") || strings.Contains(logged, "token") {
		t.Fatalf("log contains sensitive query data: %s", logged)
	}
}
