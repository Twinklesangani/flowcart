package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		errorCode := ""
		if status >= http.StatusBadRequest {
			errorCode = "http_" + strconv.Itoa(status)
		}
		slog.Info("http request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"route", chi.RouteContext(r.Context()).RoutePattern(),
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
			"error_code", errorCode,
		)
	})
}
