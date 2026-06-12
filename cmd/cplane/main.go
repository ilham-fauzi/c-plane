package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ilham/c-plane/internal/api"
	"github.com/ilham/c-plane/internal/store/sqlitestore"
)

func main() {
	addr := env("CPLANE_ADDR", ":8080")
	dbPath := env("CPLANE_DB_PATH", "cplane.db")

	store, err := sqlitestore.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &http.Server{
		Addr:              addr,
		Handler:           api.NewServer(store),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("c-plane API listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
