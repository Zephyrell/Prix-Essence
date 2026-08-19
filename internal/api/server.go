// Package api expose les endpoints HTTP de l'application et sers le frontend
// embarqué (go:embed).
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zephyrell/prix-essence/internal/db"
	"github.com/zephyrell/prix-essence/internal/scheduler"
	webui "github.com/zephyrell/prix-essence"
)

// Server regroupe les dépendances des handlers HTTP.
type Server struct {
	store *db.Store
	sched *scheduler.Scheduler
	start time.Time
}

// New crée un Serveur.
func New(store *db.Store, sched *scheduler.Scheduler) *Server {
	return &Server{store: store, sched: sched, start: time.Now()}
}

// Routes construit le routeur HTTP (patterns Go 1.22+).
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/stations", s.handleStations)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/cities", s.handleCities)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.Handle("GET /", s.spaHandler())
	return mux
}

// ---- status ----

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	refreshing, _, nextRun := s.sched.Status()

	nbStations, _ := s.store.StationCount(ctx)
	nbWithFuel, _ := s.store.StationWithFuelCount(ctx)

	lastImport, _ := s.store.MetaGet(ctx, "last_import_at")
	lastMod, _ := s.store.MetaGet(ctx, "last_modified")
	sourceURL, _ := s.store.MetaGet(ctx, "source_url")

	writeJSON(w, http.StatusOK, map[string]any{
		"nb_stations":                 nbStations,
		"nb_stations_avec_carburant":  nbWithFuel,
		"data_date":                   lastMod,
		"last_refresh":                lastImport,
		"source_url":                  sourceURL,
		"refreshing":                  refreshing,
		"next_refresh":                fmtTime(nextRun),
		"db_size_bytes":               s.store.DBSizeBytes(),
		"uptime_seconds":              int64(time.Since(s.start).Seconds()),
	})
}

// ---- stations ----

// handleStations renvoie les stations avec le prix du carburant filtré.
// Paramètres : fuel (défaut Gazole), lat, lon, radius (km, défaut 20), limit (défaut 300).
// Sans lat/lon, renvoie les stations de toute la base (vue globale).
func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fuel := r.URL.Query().Get("fuel")
	if fuel == "" {
		fuel = "Gazole"
	}

	limit := 300
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	lat, lon, hasPoint, err := parsePoint(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var views []db.StationView
	if hasPoint {
		radiusKm := 20.0
		if v := r.URL.Query().Get("radius"); v != "" {
			if n, err2 := strconv.ParseFloat(v, 64); err2 == nil && n > 0 && n <= 200 {
				radiusKm = n
			}
		}
		views, err = s.store.StationsInRadius(ctx, lat, lon, radiusKm)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		// Vue globale : toutes les stations avec ce carburant (triées par ville).
		views, err = s.store.AllStationsByFuel(ctx, fuel, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	out := make([]any, 0, len(views))
	for _, v := range views {
		p := findFuel(v.Fuels, fuel)
		out = append(out, map[string]any{
			"id":          v.ID,
			"adresse":     v.Adresse,
			"ville":       v.Ville,
			"cp":          v.CP,
			"lat":         v.Latitude,
			"lon":         v.Longitude,
			"prix":        p.prix,
			"maj":         p.maj,
			"distance_km": v.DistanceKm,
		})
	}
	if out == nil {
		out = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stations": out})
}

type fuelPrice struct {
	prix float64
	maj  string
}

func findFuel(fuels []db.Fuel, nom string) fuelPrice {
	for _, f := range fuels {
		if f.Nom == nom {
			return fuelPrice{prix: f.Prix, maj: f.Maj}
		}
	}
	return fuelPrice{}
}

func parsePoint(r *http.Request) (lat, lon float64, ok bool, err error) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	if latStr == "" || lonStr == "" {
		return 0, 0, false, nil
	}
	lat, err = strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, false, errString("lat invalide")
	}
	lon, err = strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, false, errString("lon invalide")
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false, errString("coordonnées hors limites")
	}
	return lat, lon, true, nil
}

type strErr string

func (e strErr) Error() string { return string(e) }
func errString(s string) error { return strErr(s) }

// ---- history ----

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	station := r.URL.Query().Get("station")
	fuel := r.URL.Query().Get("fuel")
	if fuel == "" {
		fuel = "Gazole"
	}
	if station == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "paramètre station requis"})
		return
	}
	points, err := s.store.History(ctx, station, db.NormalizeFuel(fuel), 14)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(points))
	for _, p := range points {
		out = append(out, map[string]any{"jour": p.Jour, "prix": p.Prix})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

// ---- cities ----

func (s *Server) handleCities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"cities": []any{}})
		return
	}
	cities, err := s.store.Cities(r.Context(), strings.ToLower(q), 10)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(cities))
	for _, c := range cities {
		out = append(out, map[string]any{"ville": c.Ville, "cp": c.CP, "lat": c.Latitude, "lon": c.Longitude})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cities": out})
}

// ---- refresh ----

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.sched.RefreshNow(r.Context()); err != nil {
		if scheduler.IsAlreadyRefreshing(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "refresh déjà en cours", "message": "Import déjà en cours"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"ok":      "true",
		"message": "Import déclenché (arrivée dans quelques secondes)",
	})
}

// ---- SPA ----

// spaHandler sert les fichiers embarqués via go:embed, avec repli vers index.html
// pour toute route inconnue (deep link).
func (s *Server) spaHandler() http.Handler {
	sub, _ := fs.Sub(webui.FS, "web")
	fsrv := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := sub.Open(p)
		if err == nil {
			fi, err2 := f.Stat()
			f.Close()
			if err2 == nil && !fi.IsDir() {
				fsrv.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

// writeJSON sérialise v en JSON.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
