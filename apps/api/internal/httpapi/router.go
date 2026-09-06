package httpapi

import (
	"encoding/json"
	"net/http"

	"flowcart/apps/api/internal/auth"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/product"
	"flowcart/apps/api/internal/warehouse"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
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
	organizationRepository := organization.NewRepository(pool)
	organizationService := organization.NewService(organizationRepository)
	organizationHandler := organization.NewHandler(organizationService)
	productHandler := product.NewHandler(product.NewService(product.NewRepository(pool)))
	warehouseHandler := warehouse.NewHandler(warehouse.NewService(warehouse.NewRepository(pool)))
	inventoryHandler := inventory.NewHandler(inventory.NewService(inventory.NewRepository(pool)))
	authenticated := router.With(authService.Authenticate)
	authenticated.Post("/api/v1/organizations", organizationHandler.Create)
	authenticated.Get("/api/v1/organizations", organizationHandler.List)
	organizationMembership := organization.Membership(organizationService, func(request *http.Request) (uuid.UUID, error) {
		return uuid.Parse(chi.URLParam(request, "organizationID"))
	})
	organizationRoutes := router.With(authService.Authenticate, organizationMembership)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}", organizationHandler.Get)
	organizationRoutes.Patch("/api/v1/organizations/{organizationID}", organizationHandler.Update)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/members", organizationHandler.Members)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/members", organizationHandler.AddMember)
	organizationRoutes.Patch("/api/v1/organizations/{organizationID}/members/{userID}", organizationHandler.ChangeRole)
	organizationRoutes.Delete("/api/v1/organizations/{organizationID}/members/{userID}", organizationHandler.RemoveMember)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/products", productHandler.Create)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/products", productHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/products/{productID}", productHandler.Get)
	organizationRoutes.Patch("/api/v1/organizations/{organizationID}/products/{productID}", productHandler.Update)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/warehouses", warehouseHandler.Create)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/warehouses", warehouseHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/warehouses/{warehouseID}", warehouseHandler.Get)
	organizationRoutes.Patch("/api/v1/organizations/{organizationID}/warehouses/{warehouseID}", warehouseHandler.Update)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/inventory", inventoryHandler.Create)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/inventory", inventoryHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/inventory/{inventoryID}", inventoryHandler.Get)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/inventory/{inventoryID}/adjust", inventoryHandler.Adjust)

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
