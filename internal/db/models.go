// Package db expose les modèles de données et le store SQLite.
package db

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Fuel représente un carburant et son prix pour une station.
type Fuel struct {
	Nom  string  `json:"nom"`
	Prix float64 `json:"prix"`
	Maj  string  `json:"maj"`
}

// UnmarshalJSON accepte à la fois le format officiel {"nom","valeur","maj"} et
// un alias éventuel {"prix"}.
func (f *Fuel) UnmarshalJSON(b []byte) error {
	var r struct {
		Nom     string    `json:"nom"`
		Prix    FlexFloat `json:"valeur"`
		PrixAlt FlexFloat `json:"prix"`
		Maj     string    `json:"maj"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	f.Nom = NormalizeFuel(r.Nom)
	f.Prix = float64(r.Prix)
	if f.Prix == 0 {
		f.Prix = float64(r.PrixAlt)
	}
	f.Maj = r.Maj
	return nil
}

// Station est une station-service avec ses carburants. Son UnmarshalJSON
// accepte le format moderne (carburants[]) et le format historique à colonnes
// plates (gazole_prix, sp95_prix, …).
type Station struct {
	ID        string  `json:"id"`
	Adresse   string  `json:"adresse"`
	Ville     string  `json:"ville"`
	CP        string  `json:"code_postal"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Fuels     []Fuel  `json:"carburants"`
}

// geoCoord permet de lire la géométrie GeoJSON (features[].geometry.coordinates
// = [lon, lat]) en complément des champs latitude/longitude.
type geoCoord struct {
	Coordinates []FlexFloat `json:"coordinates"`
}

// geomPoint lit l'objet geom des exports data.economie.gouv.fr, qui porte les
// vraies coordonnées en degrés (les champs latitude/longitude y sont en
// Lambert-93, exprimés en mètres, et donc inutilisables directement).
type geomPoint struct {
	Lat FlexFloat `json:"lat"`
	Lon FlexFloat `json:"lon"`
}

func (s *Station) UnmarshalJSON(b []byte) error {
	var r struct {
		Adresse      string     `json:"adresse"`
		Ville        string     `json:"ville"`
		CP           string     `json:"cp"`
		CPCodePostal string     `json:"code_postal"`
		Latitude     FlexFloat  `json:"latitude"`
		Longitude    FlexFloat  `json:"longitude"`
		Geom         geomPoint  `json:"geom"`
		Geometry     geoCoord   `json:"geometry"`
		Fuels        []Fuel     `json:"carburants"`
		GazolePrix   FlexFloat  `json:"gazole_prix"`
		GazoleMaj    string     `json:"gazole_maj"`
		SP95Prix     FlexFloat  `json:"sp95_prix"`
		SP95Maj      string     `json:"sp95_maj"`
		SP98Prix     FlexFloat  `json:"sp98_prix"`
		SP98Maj      string     `json:"sp98_maj"`
		E10Prix      FlexFloat  `json:"e10_prix"`
		E10Maj       string     `json:"e10_maj"`
		E85Prix      FlexFloat  `json:"e85_prix"`
		E85Maj       string     `json:"e85_maj"`
		GPLcPrix     FlexFloat  `json:"gplc_prix"`
		GPLcMaj      string     `json:"gplc_maj"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}

	s.Adresse, s.Ville = r.Adresse, r.Ville
	// Le flux officiel data.economie.gouv.fr nomme la colonne "cp"
	// (les anciens exports usa< code_postal) → on accepte les deux, cp prioritaire.
	s.CP = r.CP
	if s.CP == "" {
		s.CP = r.CPCodePostal
	}
	// L'id peut être un nombre (80570001) ou une chaîne → décodage explicite.
	s.ID = decodeID(b)

	// Coordonnées : priorité à geom (degrés) puis latitude/longitude (degrés),
	// puis GeoJSON geometry.coordinates = [lon, lat].
	s.Latitude, s.Longitude = float64(r.Latitude), float64(r.Longitude)
	if float64(r.Geom.Lat) != 0 || float64(r.Geom.Lon) != 0 {
		s.Latitude, s.Longitude = float64(r.Geom.Lat), float64(r.Geom.Lon)
	}
	if len(r.Geometry.Coordinates) == 2 && s.Latitude == 0 && s.Longitude == 0 {
		s.Longitude = float64(r.Geometry.Coordinates[0])
		s.Latitude = float64(r.Geometry.Coordinates[1])
	}

	s.Fuels = r.Fuels
	if len(s.Fuels) == 0 {
		// Format historique à colonnes plates : reconstruit le tableau.
		add := func(nom string, p FlexFloat, maj string) {
			if float64(p) > 0 {
				s.Fuels = append(s.Fuels, Fuel{Nom: nom, Prix: float64(p), Maj: maj})
			}
		}
		add("Gazole", r.GazolePrix, r.GazoleMaj)
		add("SP95", r.SP95Prix, r.SP95Maj)
		add("SP98", r.SP98Prix, r.SP98Maj)
		add("E10", r.E10Prix, r.E10Maj)
		add("E85", r.E85Prix, r.E85Maj)
		add("GPLc", r.GPLcPrix, r.GPLcMaj)
	}
	for i := range s.Fuels {
		s.Fuels[i].Nom = NormalizeFuel(s.Fuels[i].Nom)
	}
	return nil
}

// NormalizeFuel normalise le nom d'un carburant vers la forme canonique
// utilisée par l'app : "Gazole", "SP95", "SP98", "E10", "E85", "GPLc".
func NormalizeFuel(n string) string {
	switch {
	case n == "":
		return ""
	case n == "Gazole" || n == "gazole" || n == "gaz":
		return "Gazole"
	case n == "SP95" || n == "sp95-e5" || strings.EqualFold(n, "sp95"):
		return "SP95"
	case n == "E10" || n == "e10":
		return "E10"
	case n == "SP98" || n == "sp98-e5" || strings.EqualFold(n, "sp98"):
		return "SP98"
	case n == "E85" || n == "e85" || n == "superethanol-e85" || n == "superéthanol-e85" || strings.Contains(strings.ToLower(n), "superet") && strings.HasSuffix(strings.ToLower(n), "e85"):
		return "E85"
	case n == "GPLc" || n == "gplc" || n == "gpl-c" || strings.EqualFold(n, "GPLc") || strings.Contains(strings.ToLower(n), "gpl"):
		return "GPLc"
	}
	return n
}

// decodeID extrait une valeur d'identifiant depuis un JSON brut, qu'elle soit
// un nombre (80570001) ou une chaîne ("80570001").
func decodeID(raw []byte) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	id, ok := m["id"]
	if !ok {
		return ""
	}
	id = bytes.TrimSpace(id)
	if len(id) > 0 && id[0] == '"' {
		var s string
		if err := json.Unmarshal(id, &s); err != nil {
			return ""
		}
		return s
	}
	// nombre (ou autre) → représentation canonique.
	var n json.Number
	if err := json.Unmarshal(id, &n); err != nil {
		return string(id)
	}
	return n.String()
}

// FlexFloat accepte un nombre JSON "1.85" ou 1.85 ou null, et tolère la virgule.
type FlexFloat float64

func (f *FlexFloat) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.ReplaceAll(s, ",", ".")
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return err
		}
		*f = FlexFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = FlexFloat(v)
	return nil
}

// HistoryPoint est un point de l'historique d'un prix pour une station.
type HistoryPoint struct {
	Jour      string  `json:"jour"` // YYYY-MM-DD (heure de Paris)
	Carburant string  `json:"carburant"`
	Prix      float64 `json:"prix"`
}

// Status résume l'état courant du service.
type Status struct {
	Stations           int    `json:"stations"`
	StationsWithFuel   int    `json:"stations_with_fuel"`
	LastImportAt       string `json:"last_import_at"`
	LastModified       string `json:"last_modified"`
	SourceURL          string `json:"source_url"`
	Refreshing         bool   `json:"refreshing"`
	NextRefresh        string `json:"next_refresh"`
	DBSizeBytes        int64  `json:"db_size_bytes"`
	UptimeSecs         int64  `json:"uptime_seconds"`
}

// ImportStats donne un décompte d'un import.
type ImportStats struct {
	StationsSeen   int
	StationsKept   int
	PricesUpserted int
	HistoryWritten int
	HistoryDeleted int
}
