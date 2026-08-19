// Command server est le point d'entrée de Prix-Essence : il monte la base
// SQLite, le planificateur et le serveur HTTP.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zephyrell/prix-essence/internal/api"
	"github.com/zephyrell/prix-essence/internal/config"
	"github.com/zephyrell/prix-essence/internal/db"
	"github.com/zephyrell/prix-essence/internal/fetcher"
	"github.com/zephyrell/prix-essence/internal/scheduler"
)

func main() {
	cfg := config.Load()

	// S'assure que le répertoire de la base existe.
	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	store, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db : %v", err)
	}
	defer store.Close()

	f := fetcher.New()
	sched := scheduler.New(store, f, cfg.Refresh)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sched.Start(ctx)

	srv := api.New(store, sched)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Prix-Essence prêt sur http://%s (db : %s)", cfg.ListenAddr, cfg.DBPath)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http : %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	log.Println("arrêt propre")
}
