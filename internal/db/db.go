package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zephyrell/prix-essence/internal/geo"
	_ "modernc.org/sqlite" // driver SQLite pure-Go (pas de CGO)
	_ "time/tzdata"        // embarque la timezone Europe/Paris dans le binaire
)

// schema crée les tables et index au premier lancement.
const schema = `
CREATE TABLE IF NOT EXISTS stations (
    id         TEXT PRIMARY KEY,
    adresse    TEXT NOT NULL DEFAULT '',
    ville      TEXT NOT NULL DEFAULT '',
    cp         TEXT NOT NULL DEFAULT '',
    latitude   REAL NOT NULL DEFAULT 0,
    longitude  REAL NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE IF NOT EXISTS prix (
    station_id TEXT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    carburant  TEXT NOT NULL,
    prix       REAL NOT NULL,
    maj        TEXT NOT NULL,
    PRIMARY KEY (station_id, carburant)
) STRICT;

CREATE TABLE IF NOT EXISTS historique (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id TEXT NOT NULL,
    carburant  TEXT NOT NULL,
    jour       TEXT NOT NULL,          -- YYYY-MM-DD (heure de Paris)
    prix       REAL NOT NULL,
    maj        TEXT NOT NULL,
    UNIQUE (station_id, carburant, jour)
) STRICT;

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_stations_lat  ON stations(latitude);
CREATE INDEX IF NOT EXISTS idx_historique_jour ON historique(jour);
`

const (
	metaLastImport = "last_import_at"
	metaLastMod    = "last_modified"
	metaSourceURL  = "source_url"
	metaStationCount = "station_count"
)

// Store encapsule l'accès à la base SQLite et les opérations associées.
type Store struct {
	db   *sql.DB
	path string
}

// Open ouvre (ou crée) la base au chemin donné, applique le schéma et règle
// les PRAGMAs de façon optimale pour SQLite en accès concurrent (WAL).
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		path,
	)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetConnMaxLifetime(0)

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: sqlDB, path: path}, nil
}

// Close ferme la base de données.
func (s *Store) Close() error { return s.db.Close() }

// Ping vérifie que la base répond.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Import remplace les données courantes par les stations données, dans une
// seule transaction (un seul fsync + rollback possible à mi-chemin). Met à jour
// l'historique de façon conditionnelle et purge les jours hors fenêtre de 14 jours.
func (s *Store) Import(ctx context.Context, stations []Station, ts time.Time, sourceURL, lastModified string) (ImportStats, error) {
	var stats ImportStats
	stats.StationsSeen = len(stations)

	paris, _ := time.LoadLocation("Europe/Paris")
	today := time.Now().In(paris)
	cutoff := today.AddDate(0, 0, -13).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin tx: %w", err)
	}
	// Si une erreur survient en cours de route, on rollback.
	defer tx.Rollback()
	txctx := context.Background() // statements préparés liés à la tx

	// 1. Purge de l'historique hors fenêtre, AVANT les inserts (libère les index).
	if res, err := tx.ExecContext(txctx, `DELETE FROM historique WHERE jour < ?`, cutoff); err == nil {
		if n, err2 := res.RowsAffected(); err2 == nil {
			stats.HistoryDeleted = int(n)
		}
	} else {
		return stats, fmt.Errorf("purge historique: %w", err)
	}

	upsertStation, err := tx.PrepareContext(txctx, `
		INSERT INTO stations (id, adresse, ville, cp, latitude, longitude)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			adresse=excluded.adresse, ville=excluded.ville, cp=excluded.cp,
			latitude=excluded.latitude, longitude=excluded.longitude`)
	if err != nil {
		return stats, fmt.Errorf("prepare station: %w", err)
	}
	defer upsertStation.Close()

	upsertPrix, err := tx.PrepareContext(txctx, `
		INSERT INTO prix (station_id, carburant, prix, maj)
		VALUES (?,?,?,?)
		ON CONFLICT(station_id, carburant) DO UPDATE SET prix=excluded.prix, maj=excluded.maj`)
	if err != nil {
		return stats, fmt.Errorf("prepare prix: %w", err)
	}
	defer upsertPrix.Close()

	lastHist, err := tx.PrepareContext(txctx, `
		SELECT prix FROM historique
		WHERE station_id=? AND carburant=?
		ORDER BY jour DESC, id DESC LIMIT 1`)
	if err != nil {
		return stats, fmt.Errorf("prepare lastHist: %w", err)
	}
	defer lastHist.Close()

	upsertHist, err := tx.PrepareContext(txctx, `
		INSERT INTO historique (station_id, carburant, jour, prix, maj)
		VALUES (?,?,?,?,?)
		ON CONFLICT(station_id, carburant, jour) DO UPDATE SET prix=excluded.prix, maj=excluded.maj`)
	if err != nil {
		return stats, fmt.Errorf("prepare hist: %w", err)
	}
	defer upsertHist.Close()

	upsertMeta, _ := tx.PrepareContext(txctx, `
		INSERT INTO meta (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	defer upsertMeta.Close()

	for _, st := range stations {
		if !validCoord(st.Latitude, st.Longitude) || st.ID == "" {
			continue
		}
		stats.StationsKept++
		if _, err := upsertStation.ExecContext(txctx, st.ID, st.Adresse, st.Ville, st.CP, st.Latitude, st.Longitude); err != nil {
			return stats, fmt.Errorf("upsert station %s: %w", st.ID, err)
		}
		for _, f := range st.Fuels {
			if f.Prix <= 0 || f.Nom == "" {
				continue // 0/null = non renseigné
			}
			stats.PricesUpserted++
			if _, err := upsertPrix.ExecContext(txctx, st.ID, f.Nom, f.Prix, f.Maj); err != nil {
				return stats, fmt.Errorf("upsert prix %s/%s: %w", st.ID, f.Nom, err)
			}

			jour := dayOf(f.Maj, ts)
			if jour < cutoff {
				continue
			}
			var last float64
			err := lastHist.QueryRowContext(txctx, st.ID, f.Nom).Scan(&last)
			switch {
			case err == sql.ErrNoRows:
				// première apparition du jour
				if _, err := upsertHist.ExecContext(txctx, st.ID, f.Nom, jour, f.Prix, f.Maj); err != nil {
					return stats, fmt.Errorf("upsert hist %s/%s: %w", st.ID, f.Nom, err)
				}
				stats.HistoryWritten++
			case err != nil:
				return stats, fmt.Errorf("lastHist %s/%s: %w", st.ID, f.Nom, err)
			case last != f.Prix:
				if _, err := upsertHist.ExecContext(txctx, st.ID, f.Nom, jour, f.Prix, f.Maj); err != nil {
					return stats, fmt.Errorf("upsert hist %s/%s: %w", st.ID, f.Nom, err)
				}
				stats.HistoryWritten++
			}
		}
	}

	nowStr := ts.UTC().Format(time.RFC3339)
	nums := [][2]string{
		{metaLastImport, nowStr},
		{metaLastMod, lastModified},
		{metaSourceURL, sourceURL},
		{metaStationCount, fmt.Sprint(stats.StationsKept)},
	}
	for _, kv := range nums {
		if _, err := upsertMeta.ExecContext(txctx, kv[0], kv[1]); err != nil {
			return stats, fmt.Errorf("meta %s: %w", kv[0], err)
		}
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit: %w", err)
	}
	return stats, nil
}

func validCoord(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && !(lat == 0 && lon == 0)
}

// dayOf convertit l'horodatage maj (ou ts en repli) vers le jour YYYY-MM-DD
// exprimé dans le fuseau Europe/Paris.
func dayOf(maj string, fallback time.Time) string {
	t := parseTimeFlexible(maj)
	if t.IsZero() {
		t = fallback
	}
	paris, _ := time.LoadLocation("Europe/Paris")
	return t.In(paris).Format("2006-01-02")
}

// StationView est une station avec ses carburants et sa distance au point cherché.
type StationView struct {
	Station
	DistanceKm float64 `json:"distance_km"`
}

// StationsInRadius retourne les stations ayant un prix renseigné à moins de
// radiusKm d'un point, triées par distance. Utilise une bounding box SQL puis
// un filtre haversine exact en Go.
func (s *Store) StationsInRadius(ctx context.Context, lat, lon, radiusKm float64) ([]StationView, error) {
	minLat, maxLat, minLon, maxLon := geo.BoundingBox(lat, lon, radiusKm)

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.adresse, s.ville, s.cp, s.latitude, s.longitude,
		       p.carburant, p.prix, p.maj
		FROM stations s
		JOIN prix p ON p.station_id = s.id
		WHERE s.latitude BETWEEN ? AND ? AND s.longitude BETWEEN ? AND ?
		  AND p.prix > 0
		ORDER BY s.id`, minLat, maxLat, minLon, maxLon)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer rows.Close()

	// Regroupe par station (une ligne par (station, carburant)).
	index := make(map[string]*StationView)
	var order []string
	for rows.Next() {
		var (
			id, adresse, ville, cp string
			stLat, stLon           float64
			carburant              string
			prix                   float64
			maj                    string
		)
		if err := rows.Scan(&id, &adresse, &ville, &cp, &stLat, &stLon, &carburant, &prix, &maj); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sv, ok := index[id]
		if !ok {
			sv = &StationView{
				Station: Station{ID: id, Adresse: adresse, Ville: ville, CP: cp, Latitude: stLat, Longitude: stLon},
				DistanceKm: geo.Haversine(lat, lon, stLat, stLon),
			}
			index[id] = sv
			order = append(order, id)
		}
		sv.Fuels = append(sv.Fuels, Fuel{Nom: carburant, Prix: prix, Maj: maj})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]StationView, 0, len(order))
	for _, id := range order {
		sv := index[id]
		if sv.DistanceKm <= radiusKm {
			out = append(out, *sv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceKm < out[j].DistanceKm })
	return out, nil
}

// AllStationsByFuel retourne les stations qui ont un prix pour le carburant
// donné, triées par ville (vue globale de la carte).
func (s *Store) AllStationsByFuel(ctx context.Context, fuel string, limit int) ([]StationView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.adresse, s.ville, s.cp, s.latitude, s.longitude,
		       p.carburant, p.prix, p.maj
		FROM stations s
		JOIN prix p ON p.station_id = s.id
		WHERE p.carburant = ? AND p.prix > 0
		ORDER BY s.ville, s.id
		LIMIT ?`, NormalizeFuel(fuel), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StationView, 0, limit)
	for rows.Next() {
		var (
			id, adresse, ville, cp, carburant, maj string
			stLat, stLon, prix                      float64
		)
		if err := rows.Scan(&id, &adresse, &ville, &cp, &stLat, &stLon, &carburant, &prix, &maj); err != nil {
			return nil, err
		}
		out = append(out, StationView{Station: Station{
			ID: id, Adresse: adresse, Ville: ville, CP: cp, Latitude: stLat, Longitude: stLon,
			Fuels: []Fuel{{Nom: carburant, Prix: prix, Maj: maj}},
		}})
	}
	return out, rows.Err()
}

// History retourne l'historique (jours) d'une station pour un carburant sur la
// fenêtre donnée, trié par jour croissant.
func (s *Store) History(ctx context.Context, stationID, carburant string, days int) ([]HistoryPoint, error) {
	paris, _ := time.LoadLocation("Europe/Paris")
	from := time.Now().In(paris).AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.QueryContext(ctx, `
		SELECT jour, carburant, prix FROM historique
		WHERE station_id=? AND carburant=? AND jour >= ?
		ORDER BY jour ASC`, stationID, carburant, from)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var out []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.Jour, &p.Carburant, &p.Prix); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DBSizeBytes retourne la taille du fichier de base (utile pour /api/status).
func (s *Store) DBSizeBytes() int64 {
	fi, err := os.Stat(s.path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// MetaGet lit une clé meta.
func (s *Store) MetaGet(ctx context.Context, key string) (string, bool) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

// StationCount retourne le nombre total de stations en base.
func (s *Store) StationCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stations`).Scan(&n)
	return n, err
}

// StationWithFuelCount retourne le nombre de stations ayant un prix renseigné.
func (s *Store) StationWithFuelCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT station_id) FROM prix WHERE prix > 0`).Scan(&n)
	return n, err
}

// Cities suggère des villes (uniq) pour l'autocomplete. La saisie est
// comparée de façon insensible à la casse et aux accents (matching réalisé
// en Go, SQLite ne normalisant pas les accents nativement). Les villes qui
// commencent par la saisie sont classées en premier.
func (s *Store) Cities(ctx context.Context, q string, limit int) ([]CitySuggestion, error) {
	if limit <= 0 {
		limit = 10
	}
	needle := normalizeForLike(q)

	// Récupère les (ville, cp) distincts (~5 500) agrégés, puis filtre et trie
	// en Go avec une comparaison accent/casse-insensible. Regrouper par
	// (ville, cp) (et non ville seule) préserve chaque code postal d'une grande
	// ville — Paris a 75001..75020, chercher "75010" doit le retrouver. Le volume
	// est négligeable par frappe (debounce), et on évite un filtre SQL qui raterait
	// l'insensibilité aux accents ("creteil" → Créteil).
	rows, err := s.db.QueryContext(ctx, `
		SELECT ville,
		       cp,
		       MAX(latitude), MAX(longitude)
		FROM stations
		WHERE ville <> ''
		GROUP BY lower(ville), cp
		ORDER BY ville`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perKey := map[string]*CitySuggestion{}
	var order []string
	for rows.Next() {
		var v, cp string
		var lat, lon float64
		if err := rows.Scan(&v, &cp, &lat, &lon); err != nil {
			return nil, err
		}
		norm := normalizeForLike(v)
		// Nom : préfixe OU sous-chaîne ; code postal : préfixe uniquement
		// (chercher "750" doit viser Paris, pas Buchy-76750).
		normCP := strings.ReplaceAll(normalizeForLike(cp), "-", "")
		if !strings.HasPrefix(norm, needle) && !strings.Contains(norm, needle) &&
			!strings.HasPrefix(normCP, needle) {
			continue
		}
		k := v + "|" + cp
		if _, ok := perKey[k]; ok {
			continue
		}
		perKey[k] = &CitySuggestion{Ville: v, CP: cp, Latitude: lat, Longitude: lon}
		order = append(order, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Tri : les noms qui commencent par la saisie d'abord, puis alphabétique,
	// avec un cp non vide favorisé.
	sort.SliceStable(order, func(i, j int) bool {
		vi, vj := perKey[order[i]], perKey[order[j]]
		// Une ville dont le nom commence par la saisie prime ; sinon une
		// correspondance de code postal (recherche par CP) est favorisée.
		pi := strings.HasPrefix(normalizeForLike(vi.Ville), needle)
		pj := strings.HasPrefix(normalizeForLike(vj.Ville), needle)
		if pi != pj {
			return pi
		}
		pci := strings.HasPrefix(strings.ReplaceAll(normalizeForLike(vi.CP), "-", ""), needle)
		pcj := strings.HasPrefix(strings.ReplaceAll(normalizeForLike(vj.CP), "-", ""), needle)
		if pci != pcj {
			return pci
		}
		if (vi.CP == "") != (vj.CP == "") {
			return vi.CP != ""
		}
		return vi.Ville < vj.Ville
	})
	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]CitySuggestion, 0, len(order))
	for _, k := range order {
		out = append(out, *perKey[k])
	}
	return out, nil
}

// CitySuggestion est une entrée d'autocomplete de ville.
type CitySuggestion struct {
	Ville     string  `json:"ville"`
	CP        string  `json:"cp"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
}

// normalizeForLike met en minuscules et retire les accents, pour une
// comparaison à la fois accent- et casse-insensible (HasPrefix/Contains).
func normalizeForLike(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'é', 'è', 'ê', 'ë':
			b.WriteByte('e')
		case 'à', 'â', 'ä':
			b.WriteByte('a')
		case 'î', 'ï':
			b.WriteByte('i')
		case 'ô', 'ö':
			b.WriteByte('o')
		case 'ù', 'û', 'ü':
			b.WriteByte('u')
		case 'ç':
			b.WriteByte('c')
		case 'œ':
			b.WriteString("oe")
		case ' ', '\t', '-', '\'', '’':
			// Espaces, tirets et apostrophes ignorés pour la comparaison :
			// "saint paul", "saint-paul" et "saintpaul" matchent tous "Saint-Paul".
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

