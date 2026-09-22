package httpapi

import (
	"encoding/json"
	"flowcart/apps/api/internal/httpboundary"
	"flowcart/apps/api/internal/ratelimit"
	"fmt"
	"net/http"
	"os"
	"time"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/auth"
	"flowcart/apps/api/internal/dashboard"
	"flowcart/apps/api/internal/fulfillment"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/order"
	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/payment"
	"flowcart/apps/api/internal/product"
	"flowcart/apps/api/internal/transfer"
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

func NewRouter(pool *pgxpool.Pool, authService *auth.Service, appEnv string, policies ...httpboundary.OriginPolicy) (http.Handler, error) {
	router, _, err := NewRouterWithCheckout(pool, authService, appEnv, policies...)
	return router, err
}

func NewRouterWithCheckout(pool *pgxpool.Pool, authService *auth.Service, appEnv string, policies ...httpboundary.OriginPolicy) (http.Handler, *payment.CheckoutService, error) {
	origins, _ := httpboundary.NewOriginPolicy("", appEnv == "production")
	if len(policies) > 0 {
		origins = policies[0]
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(requestLogger)
	router.Use(middleware.Recoverer)
	router.Use(apiSecurityHeaders)
	router.Use(func(next http.Handler) http.Handler { return corsMiddleware(next, origins) })

	router.Get("/health", healthHandler(pool))
	provisioningEnabled := appEnv != "production"
	authHandler := auth.NewHandlerForEnvironment(authService, appEnv == "production", provisioningEnabled, origins)
	router.Route("/api/v1/auth", func(router chi.Router) {
		router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
			})
		})
		router.Post("/register", authHandler.Register)
		router.Post("/login", authHandler.Login)
		router.Post("/refresh", authHandler.Refresh)
		router.Post("/logout", authHandler.Logout)
		router.With(authService.Authenticate).Get("/me", authHandler.Me)
	})
	organizationRepository := organization.NewRepository(pool)
	organizationService := organization.NewService(organizationRepository)
	organizationHandler := organization.NewHandlerForEnvironment(organizationService, provisioningEnabled)
	productHandler := product.NewHandler(product.NewService(product.NewRepository(pool)))
	warehouseHandler := warehouse.NewHandler(warehouse.NewService(warehouse.NewRepository(pool)))
	inventoryHandler := inventory.NewHandler(inventory.NewService(inventory.NewRepository(pool)))
	orderHandler := order.NewHandler(order.NewService(order.NewRepository(pool)))
	fulfillmentHandler := fulfillment.NewHandler(fulfillment.NewService(fulfillment.NewRepository(pool)))
	transferHandler := transfer.NewHandler(transfer.NewService(transfer.NewRepository(pool)))
	auditHandler := audit.NewHandler(audit.NewReader(pool))
	dashboardHandler := dashboard.NewHandler(dashboard.NewRepository(pool))
	paymentRepository := payment.NewRepository(pool)
	var checkoutService *payment.CheckoutService
	if os.Getenv("PAYMENTS_PROVIDER") == "stripe" {
		provider, err := payment.NewStripeProvider(os.Getenv("STRIPE_SECRET_KEY"), os.Getenv("STRIPE_WEBHOOK_SECRET"))
		if err != nil {
			return nil, nil, fmt.Errorf("configure stripe provider: %w", err)
		} else {
			checkoutService = payment.NewCheckoutService(paymentRepository, provider, os.Getenv("STRIPE_EXPECTED_LIVEMODE") == "true")
		}
	}
	paymentHandler := payment.NewHandler(payment.NewService(paymentRepository), checkoutService)
	userIdentity := func(r *http.Request) (string, bool) {
		id, ok := auth.UserIDFromContext(r.Context())
		return id.String(), ok
	}
	organizationCreationLimit := ratelimit.New(5, 10*time.Minute, ratelimit.DefaultMaxEntries, nil).Middleware(userIdentity)
	mutationLimit := ratelimit.New(30, time.Minute, ratelimit.DefaultMaxEntries, nil).Middleware(userIdentity)
	authenticated := router.With(authService.Authenticate)
	authenticated.With(organizationCreationLimit).Post("/api/v1/organizations", organizationHandler.Create)
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
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/inventory/low-stock", inventoryHandler.LowStock)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/inventory/{inventoryID}", inventoryHandler.Get)
	organizationRoutes.Patch("/api/v1/organizations/{organizationID}/inventory/{inventoryID}/replenishment-policy", inventoryHandler.UpdateReplenishmentPolicy)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/inventory/{inventoryID}/adjust", inventoryHandler.Adjust)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/inventory/{inventoryID}/reservations", inventoryHandler.Reserve)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/reservations/{reservationID}/release", inventoryHandler.Release)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/inventory/{inventoryID}/movements", fulfillmentHandler.Movements)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/transfers", transferHandler.Create)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/transfers", transferHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/transfers/{transferID}", transferHandler.Get)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/transfers/{transferID}/timeline", transferHandler.Timeline)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/transfers/{transferID}/dispatch", transferHandler.Dispatch)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/transfers/{transferID}/receive", transferHandler.Receive)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/transfers/{transferID}/cancel", transferHandler.Cancel)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/orders", orderHandler.Create)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/orders/auto-allocate", orderHandler.AutoCreate)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/orders", orderHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/orders/{orderID}", orderHandler.Get)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/orders/{orderID}/timeline", orderHandler.Timeline)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/orders/{orderID}/cancel", orderHandler.Cancel)
	organizationRoutes.Post("/api/v1/organizations/{organizationID}/orders/{orderID}/fulfillments", fulfillmentHandler.Create)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/orders/{orderID}/fulfillments", fulfillmentHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/fulfillments/{fulfillmentID}", fulfillmentHandler.Get)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/orders/{orderID}/payments", paymentHandler.Create)
	organizationRoutes.With(mutationLimit).Post("/api/v1/organizations/{organizationID}/orders/{orderID}/checkout", paymentHandler.Checkout)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/orders/{orderID}/payments", paymentHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/payments/{paymentID}", paymentHandler.Get)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/audit-events", auditHandler.List)
	organizationRoutes.Get("/api/v1/organizations/{organizationID}/dashboard", dashboardHandler.Metrics)
	router.Post("/api/v1/webhooks/stripe", paymentHandler.Webhook)

	return router, checkoutService, nil
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

func corsMiddleware(next http.Handler, policies ...httpboundary.OriginPolicy) http.Handler {
	origins, _ := httpboundary.NewOriginPolicy("", false)
	if len(policies) > 0 {
		origins = policies[0]
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		writer.Header().Add("Vary", "Origin")
		if len(request.Header.Values("Origin")) == 1 && origins.TrustedOrigin(origin) {
			writer.Header().Set("Access-Control-Allow-Origin", origin)

			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(writer, request)
	})
}
