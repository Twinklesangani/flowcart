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

	"flowcart/apps/api/internal/config"
	"flowcart/apps/api/internal/httpapi"
)

func main() {
	cfg := config.Load()
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: httpapi.NewRouter(),
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server listening on http://localhost:%s", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case signal := <-shutdownSignals:
		log.Printf("shutdown signal received: %s", signal)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
}
