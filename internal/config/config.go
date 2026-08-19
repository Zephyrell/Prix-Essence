// Package config lit la configuration via flags et variables d'environnement.
package config

import (
	"flag"
	"os"
	"time"
)

// Config regroupe le paramétrage du service.
type Config struct {
	ListenAddr   string
	DBPath       string
	RefreshOnStart bool
	Refresh      time.Duration
}

// Load résout la configuration. Priorité : flag > env > défaut.
func Load() Config {
	var cfg Config

	flag.StringVar(&cfg.ListenAddr, "listen", "localhost:8080", "adresse d'écoute HTTP")
	flag.StringVar(&cfg.DBPath, "db", "data/prix-essence.db", "chemin de la base SQLite")
	flag.BoolVar(&cfg.RefreshOnStart, "refresh-on-start", true, "importer au démarrage")
	flag.DurationVar(&cfg.Refresh, "refresh", 30*time.Minute, "intervalle de rafraîchissement")
	flag.Parse()

	// Environnement : valeurs explicites non vides écrasent les défauts.
	if v := os.Getenv("LISTEN"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("REFRESH_ON_START"); v != "" {
		cfg.RefreshOnStart = v == "1" || v == "true"
	}
	if v := os.Getenv("REFRESH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Refresh = d
		}
	}
	return cfg
}
