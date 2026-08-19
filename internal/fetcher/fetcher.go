// Package fetcher télécharge et parse les prix de carburants depuis le jeu de
// données officiel de l'État (data.gouv.fr).
package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zephyrell/prix-essence/internal/db"
)

const (
	// datasetURL est la page API du jeu de données officiel « Prix des
	// carburants en France - Flux instantané v2 » (data.economie.gouv.fr,
	// Ministère de l'Économie) qui expose la liste des ressources à télécharger.
	// (Slug historique « prix-des-carburants-en-francetemps-reel » retiré fin 2024.)
	datasetURL = "https://www.data.gouv.fr/api/1/datasets/prix-des-carburants-en-france-flux-instantane-v2-amelioree/"
	// userAgent identifie notre service auprès de data.gouv.fr (charte de politesse).
	// Remplace l'adresse de contact par la tienne.
	userAgent = "PrixEssence/1.0 (prix-essence; contact@example.org)"
	// maxBodyBytes borne la taille des fichiers téléchargés (~13k stations < 20 Mo).
	maxBodyBytes = 64 << 20
)

// Resource est une source téléchargeable (JSON ou CSV temps réel).
type Resource struct {
	URL          string
	Format       string // "json" | "csv"
	LastModified string // RFC3339 (nullable)
}

// Fetcher résout puis télécharge la ressource de prix courante.
type Fetcher struct {
	client *http.Client
}

// New crée un Fetcher avec un client HTTP muni d'un timeout raisonnable.
func New() *Fetcher {
	return &Fetcher{client: &http.Client{Timeout: 30 * time.Second}}
}

// Resolve interroge l'API data.gouv.fr pour trouver l'URL de la ressource
// « temps réel » la plus récente (JSON préféré, repli CSV).
func (f *Fetcher) Resolve(ctx context.Context) (Resource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, datasetURL, nil)
	if err != nil {
		return Resource{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return Resource{}, fmt.Errorf("resolve dataset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Resource{}, fmt.Errorf("resolve dataset: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Resources []struct {
			Format       string `json:"format"`
			URL          string `json:"url"`
			LastModified string `json:"last_modified"`
		} `json:"resources"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return Resource{}, fmt.Errorf("decode resources: %w", err)
	}

	best := Resource{}
	bestTime := time.Time{}
	for _, r := range payload.Resources {
		format := normalizeFormat(r.Format)
		if format != "json" && format != "csv" {
			continue // ignore les exports .xml/.shp
		}
		// Le flux officiel annonce plusieurs ressources "json" : la simple
		// (/exports/json) et la géométrique (/exports/geojson). On préfère la
		// simple dès que son URL le signale (elle est plus facile à parser).
		if strings.Contains(r.URL, "/geojson") {
			continue
		}
		lm, _ := time.Parse(time.RFC3339, r.LastModified)
		if lm.IsZero() {
			lm = time.Now() // ressource sans date : considérée comme candidate
		}
		// À date égale, préfère le JSON simple (plus facile à parser que le CSV).
		if lm.After(bestTime) || (lm.Equal(bestTime) && format == "json") {
			best = Resource{URL: r.URL, Format: format, LastModified: r.LastModified}
			bestTime = lm
		}
	}
	if best.URL == "" {
		return Resource{}, fmt.Errorf("aucune ressource téléchargeable trouvée")
	}
	return best, nil
}

func normalizeFormat(f string) string {
	switch f {
	case "json", "JSON", "application/json":
		return "json"
	case "geojson", "GeoJSON":
		return "geojson"
	case "csv", "CSV", "text/csv":
		return "csv"
	}
	return f
}

// Fetch télécharge le contenu d'une ressource et le parse en stations.
// Retourne les stations, l'horodatage de référence et l'erreur éventuelle.
func (f *Fetcher) Fetch(ctx context.Context, res Resource) ([]db.Station, time.Time, error) {
	raw, err := fetchBody(ctx, f.client, res.URL)
	if err != nil {
		return nil, time.Time{}, err
	}

	var (
		stations []db.Station
		parseErr error
	)
	if res.Format == "csv" {
		stations, parseErr = parseCSV(raw)
	} else {
		stations, parseErr = parseJSON(raw)
		// repli automatique sur le CSV si le JSON a échoué mais que c'est bien du CSV.
		if parseErr != nil && looksLikeCSV(raw) {
			if csv, csvErr := parseCSV(raw); csvErr == nil {
				stations, parseErr = csv, nil
			}
		}
	}
	if parseErr != nil {
		return nil, time.Time{}, parseErr
	}

	ts := time.Now().UTC()
	return stations, ts, nil
}
