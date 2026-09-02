package httpapi

import (
	"encoding/json"
	"net/http"

	"flowcart/apps/api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type healthResponse struct {
	Status   string `json:"status"`
	Service  string `json:"service"`
	Database string `json:"database"`
}

func NewRouter(pool *pgxpool.Pool, authService *auth.Service, appEnv string) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware)

	router.Get("/health", healthHandler(pool))
	authHandler := auth.NewHandler(authService, appEnv == "production")
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.Post("/register", authHandler.Register)
		router.Post("/login", authHandler.Login)
		router.Post("/refresh", authHandler.Refresh)
		router.Post("/logout", authHandler.Logout)
		router.With(authService.Authenticate).Get("/me", authHandler.Me)
	})

	return router
}

func healthHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := pool.Ping(request.Context()); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(writer).Encode(healthResponse{
				Status:   "unhealthy",
				Service:  "flowcart-api",
				Database: "unavailable",
			})
			return
		}

		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(healthResponse{
			Status:   "ok",
			Service:  "flowcart-api",
			Database: "ok",
		})
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://localhost:3001" {
			writer.Header().Set("Access-Control-Allow-Origin", origin)
			writer.Header().Set("Vary", "Origin")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(writer, request)
	})
}
