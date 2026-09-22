package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	database, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		log.Fatalf("create migration runner: %v", err)
	}
	defer database.Close()

	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./cmd/migrate [up|down]")
	}

	switch os.Args[1] {
	case "up":
		err = database.Up()
	case "down":
		err = database.Steps(-1)
	default:
		log.Fatalf("unknown migration command %q; use up or down", os.Args[1])
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatal(fmt.Errorf("run migration %s: %w", os.Args[1], err))
	}

	if errors.Is(err, migrate.ErrNoChange) {
		log.Printf("migration %s: no change", os.Args[1])
		return
	}

	log.Printf("migration %s completed", os.Args[1])
}
