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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := platform.OpenDBFromEnv(ctx)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := migrate.Up(ctx, db.Pool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	log.Println("migrations applied")
	os.Exit(0)
}
