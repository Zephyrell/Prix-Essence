package fetcher

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/zephyrell/prix-essence/internal/db"
)

// parseJSON décode une liste de stations dans plusieurs formes possibles :
//   - tableau direct        : [ {...}, ... ]
//   - wrapper objet         : {"stations":[...]} / {"records":[...]} / {"results":[...]}
//   - station unique        : {...}
//   - GeoJSON FeatureCollection : {"features":[{properties, geometry}], ...}
func parseJSON(data []byte) ([]db.Station, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("données JSON vides")
	}
	switch trimmed[0] {
	case '[':
		var out []db.Station
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("parse json array: %w", err)
		}
		return out, nil

	case '{':
		// 1. Wrappers connus.
		for _, key := range []string{"stations", "records", "results"} {
			var w struct {
				List []db.Station `json:"stations"`
				// note : records/results réutilisent le même champ via rename ci-dessous
			}
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(trimmed, &probe); err != nil {
				return nil, fmt.Errorf("probe wrapper: %w", err)
			}
			if raw, ok := probe[key]; ok {
				if err := json.Unmarshal(raw, &w.List); err != nil {
					// le wrapper existe mais le contenu n'est pas une liste de stations
					continue
				}
				return w.List, nil
			}
			_ = w
		}

		// 2. GeoJSON FeatureCollection.
		if isGeoJSON(trimmed) {
			return parseGeoJSON(trimmed)
		}

		// 3. Station unique.
		var s db.Station
		if err := json.Unmarshal(trimmed, &s); err == nil && (s.ID != "" || s.Latitude != 0) {
			return []db.Station{s}, nil
		}

		return nil, fmt.Errorf("format JSON non reconnu (objet)")

	case 'n': // "null"
		return nil, nil
	}
	return nil, fmt.Errorf("format JSON non reconnu")
}

// isGeoJSON détecte un FeatureCollection via la présence de la clé "features".
func isGeoJSON(trimmed []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return false
	}
	_, ok := probe["features"]
	return ok
}

// parseGeoJSON convertit une FeatureCollection GeoJSON en stations.
// Chaque feature : {type:"Feature", properties:{...}, geometry:{coordinates:[lon,lat]}}.
func parseGeoJSON(data []byte) ([]db.Station, error) {
	var fc struct {
		Features []struct {
			Type       string `json:"type"`
			Properties struct {
				ID      json.RawMessage `json:"id"`
				Adresse string          `json:"adresse"`
				Ville   string          `json:"ville"`
				CP      string          `json:"cp"`
				CPCode  string          `json:"code_postal"`
				Fuels   []db.Fuel       `json:"carburants"`
			} `json:"properties"`
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse geojson: %w", err)
	}
	out := make([]db.Station, 0, len(fc.Features))
	for _, f := range fc.Features {
		cp := f.Properties.CP
		if cp == "" {
			cp = f.Properties.CPCode
		}
		st := db.Station{
			ID:      rawIDToString(f.Properties.ID),
			Adresse: f.Properties.Adresse,
			Ville:   f.Properties.Ville,
			CP:      cp,
			Fuels:   f.Properties.Fuels,
		}
		if len(f.Geometry.Coordinates) == 2 {
			st.Longitude = f.Geometry.Coordinates[0]
			st.Latitude = f.Geometry.Coordinates[1]
		}
		out = append(out, st)
	}
	return out, nil
}

// rawIDToString convertit un id JSON ("123" ou 123) en chaîne.
func rawIDToString(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	id = bytes.TrimSpace(id)
	if id[0] == '"' {
		var s string
		if err := json.Unmarshal(id, &s); err == nil {
			return s
		}
		return ""
	}
	return string(id)
}

// looksLikeCSV indique si les données ressemblent à du CSV (première ligne
// avec plusieurs champs séparés par ';' ou ',').
func looksLikeCSV(data []byte) bool {
	first := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		first = data[:i]
	}
	sc := bytes.Count(first, []byte{';'})
	cc := bytes.Count(first, []byte{','})
	return sc > 2 || cc > 2
}
