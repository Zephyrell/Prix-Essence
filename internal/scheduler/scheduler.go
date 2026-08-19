// Package scheduler orchestre le rafraîchissement périodique des données.
package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/zephyrell/prix-essence/internal/db"
	"github.com/zephyrell/prix-essence/internal/fetcher"
)

// Scheduler déclenche un import au démarrage puis toutes les `interval`,
// sans jamais lancer deux imports simultanés.
type Scheduler struct {
	store    *db.Store
	f        *fetcher.Fetcher
	interval time.Duration

	mu         sync.Mutex
	refreshing bool
	lastRun    time.Time // tiens à jour de façon best-effort
	nextRun    time.Time
}

// New crée un Scheduler.
func New(store *db.Store, f *fetcher.Fetcher, interval time.Duration) *Scheduler {
	return &Scheduler{
		store:    store,
		f:        f,
		interval: interval,
		nextRun:  time.Now().Add(interval),
	}
}

// Start lance le rafraîchissement initial (asynchrone pour ne pas bloquer le
// serveur HTTP) puis boucle sur un ticker.
func (s *Scheduler) Start(ctx context.Context) {
	// Import initial en arrière-plan : la carte se peuple quand les données arrivent.
	go s.refresh(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.refresh(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// refresh exécute un import complet, sauf si un autre est déjà en cours.
func (s *Scheduler) refresh(ctx context.Context) {
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return
	}
	s.refreshing = true
	s.lastRun = time.Now()
	s.nextRun = time.Now().Add(s.interval)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.refreshing = false
		s.mu.Unlock()
	}()

	if err := s.runOnce(ctx); err != nil {
		log.Printf("[scheduler] import échoué : %v", err)
	}
}

// runOnce résout la ressource, télécharge et importe en base.
func (s *Scheduler) runOnce(ctx context.Context) error {
	res, err := s.f.Resolve(ctx)
	if err != nil {
		return err
	}
	stations, ts, err := s.f.Fetch(ctx, res)
	if err != nil {
		return err
	}
	stats, err := s.store.Import(ctx, stations, ts, res.URL, res.LastModified)
	if err != nil {
		return err
	}
	log.Printf("[scheduler] import terminé : %d stations (%d avec carburant), %d prix, %d hist, %d purgés",
		stats.StationsSeen, stats.StationsKept, stats.PricesUpserted, stats.HistoryWritten, stats.HistoryDeleted)
	return nil
}

// RefreshNow déclenche un import immédiat (endpoint /api/refresh). Retourne
// une erreur si un import est déjà en cours.
func (s *Scheduler) RefreshNow(ctx context.Context) error {
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return errAlreadyRefreshing
	}
	s.refreshing = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.refreshing = false
			s.nextRun = time.Now().Add(s.interval)
			s.mu.Unlock()
		}()
		if err := s.runOnce(ctx); err != nil {
			log.Printf("[scheduler] refresh manuel échoué : %v", err)
		}
	}()
	return nil
}

var errAlreadyRefreshing = &RefreshError{}

// RefreshError signale qu'un import est déjà en cours.
type RefreshError struct{}

func (*RefreshError) Error() string { return "refresh déjà en cours" }

// IsAlreadyRefreshing indique si l'erreur provient d'un import concurrent.
func IsAlreadyRefreshing(err error) bool {
	_, ok := err.(*RefreshError)
	return ok
}

// Status renvoie l'état courant (pour /api/status).
func (s *Scheduler) Status() (refreshing bool, lastRun, nextRun time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshing, s.lastRun, s.nextRun
}
