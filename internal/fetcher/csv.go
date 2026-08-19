package fetcher

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zephyrell/prix-essence/internal/db"
)

// parseCSV décode les exports CSV officiels des prix de carburants.
// Gère : BOM UTF-8, délimiteur ';' (défaut) ou ',', virgule décimale et
// fichier encodé latin-1/Windows-1252.
func parseCSV(data []byte) ([]db.Station, error) {
	// Retire le BOM UTF-8 éventuel.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	// Ré-encode latin-1 → UTF-8 si besoin (anciens exports Windows-1252).
	if !utf8.Valid(data) {
		data = latin1ToUTF8(data)
	}

	delimiter := detectDelimiter(data)
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = delimiter
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // lignes hétérogènes tolérées

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("lire en-tête CSV: %w", err)
	}
	col := make(map[string]int)
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}

	var out []db.Station
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("lire ligne CSV: %w", err)
		}

		st := db.Station{
			ID:      get(rec, col, "id"),
			Adresse: get(rec, col, "adresse"),
			Ville:   get(rec, col, "ville"),
			CP:      get(rec, col, "cp", "code_postal", "code-postal"),
		}
		// Coordonnées : dans le flux officiel data.economie.gouv.fr, les colonnes
		// latitude/longitude sont en Lambert-93 (mètres) ; la vraie position est
		// dans la colonne geom, au format "lat, lon" (degrés décimaux).
		st.Latitude = parseFloatFR(get(rec, col, "latitude"))
		st.Longitude = parseFloatFR(get(rec, col, "longitude"))
		if geom := get(rec, col, "geom"); geom != "" {
			if lat, lon := parseGeomPair(geom); lat != 0 || lon != 0 {
				st.Latitude, st.Longitude = lat, lon
			}
		}

		// Carburants : colonnes "<fuel>_prix" + "<fuel>_maj".
		for _, name := range []string{"Gazole", "SP95", "SP98", "E10", "E85", "GPLc"} {
			prixKey := strings.ToLower(name) + "_prix"
			if i, ok := col[prixKey]; ok && i < len(rec) {
				p := parseFloatFR(strings.TrimSpace(rec[i]))
				if p > 0 {
					maj := ""
					if j, ok := col[strings.ToLower(name)+"_maj"]; ok && j < len(rec) {
						maj = strings.TrimSpace(rec[j])
					}
					st.Fuels = append(st.Fuels, db.Fuel{Nom: db.NormalizeFuel(name), Prix: p, Maj: maj})
				}
			}
		}
		if len(st.Fuels) > 0 {
			out = append(out, st)
		}
	}
	return out, nil
}

// detectDelimiter choisit ';' ou ',' selon le nombre d'occurrences sur la
// première ligne. Les exports français officiels utilisent ';'.
func detectDelimiter(data []byte) rune {
	first := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		first = data[:i]
	}
	if bytes.Count(first, []byte{';'}) > bytes.Count(first, []byte{','}) {
		return ';'
	}
	return ','
}

// get lit un champ par nom de colonne, avec alias possibles.
func get(rec []string, col map[string]int, names ...string) string {
	for _, n := range names {
		if i, ok := col[n]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
	}
	return ""
}

// parseFloatFR convertit "1,799" ou "1.799" en float, renvoyant 0 en cas
// d'échec. Conçu pour des champs de prix uniquement.
func parseFloatFR(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseGeomPair décode la colonne geom du flux officiel, au format "lat, lon"
// (ou un objet JSON {"lat":..,"lon":..} dans certains exports). Retourne (0,0)
// si incompatible.
func parseGeomPair(s string) (lat, lon float64) {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '{' {
		// éventuellement un objet JSON : {"lon": x, "lat": y}
		var g struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		if err := json.Unmarshal([]byte(s), &g); err == nil {
			return g.Lat, g.Lon
		}
		return 0, 0
	}
	// forme "lat, lon" — le flux officiel écrit la latitude en premier.
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) < 2 {
		return 0, 0
	}
	lat = parseFloatFR(parts[0])
	lon = parseFloatFR(parts[1])
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0
	}
	return lat, lon
}

// latin1ToUTF8 transcode du ISO-8859-1/Windows-1252 vers UTF-8.
func latin1ToUTF8(data []byte) []byte {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return []byte(b.String())
}
