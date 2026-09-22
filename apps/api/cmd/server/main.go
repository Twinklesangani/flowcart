package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flowcart/apps/api/internal/auth"
	"flowcart/apps/api/internal/config"
	"flowcart/apps/api/internal/database"
	"flowcart/apps/api/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := database.NewPool(startupContext, cfg.DatabaseURL)
	cancelStartup()
	if err != nil {
		log.Fatal("connect to PostgreSQL failed; check DATABASE_URL and database availability")
	}
	defer pool.Close()

	authRepository := auth.NewRepository(pool)
	authService := auth.NewService(authRepository, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	handler, checkoutService, err := httpapi.NewRouterWithCheckout(pool, authService, cfg.AppEnv, cfg.Origins)
	if err != nil {
		log.Fatalf("configure payment provider: %v", err)
	}
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		Addr:              ":" + cfg.Port,
		Handler:           handler,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server listening on http://localhost:%s", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	serverContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if checkoutService != nil {
		go checkoutService.Run(serverContext, func() { log.Printf("payment reconciliation failed") })
	}

	select {
	case err := <-serverErrors:
		stop()
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case <-serverContext.Done():
		log.Printf("shutdown signal received")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
}
