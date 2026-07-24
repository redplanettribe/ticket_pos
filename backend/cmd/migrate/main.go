package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"
)

func main() {
	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Wide enough to cover the connect retry window in platform.OpenDB plus the
	// migrations themselves, and still inside the Cloud Run Job's 900s task
	// timeout so a hung migration is killed by something rather than nothing.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db, err := platform.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := migrate.Up(ctx, db.Pool, cfg.AppEnv != "production"); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	log.Println("migrations applied")
	os.Exit(0)
}
