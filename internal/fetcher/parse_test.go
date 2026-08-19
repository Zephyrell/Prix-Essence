package fetcher

import (
	"testing"

	"github.com/zephyrell/prix-essence/internal/db"
)

const newFormatJSON = `[
 {"id":"A1","adresse":"1 Rue X","ville":"Nantes","code_postal":"44000",
  "latitude":47.218,"longitude":-1.553,
  "carburants":[{"nom":"Gazole","valeur":1.799,"maj":"2026-08-18T06:00:00Z"},
                {"nom":"SP95","valeur":"1.899","maj":"2026-08-18T06:00:00Z"}]}]`

const legacyJSON = `[
 {"id":"A2","adresse":"2 Rue Y","ville":"Lyon","code_postal":"69001",
  "latitude":45.764,"longitude":4.836,
  "gazole_prix":1.85,"gazole_maj":"2024-01-01T12:00:00Z",
  "sp95_prix":1.95,"sp95_maj":"2024-01-01T12:00:00Z"}]`

const wrappedJSON = `{"stations":[{"id":"A3","adresse":"3","ville":"Marseille",
  "code_postal":"13001","latitude":43.296,"longitude":5.370,"gazole_prix":1.78,
  "gazole_maj":"2025-05-01T08:00:00Z"}]}`

const geojsonJSON = `{"type":"FeatureCollection","features":[
  {"type":"Feature","properties":{"id":"G1","adresse":"5 Av","ville":"Brest","code_postal":"29200",
    "carburants":[{"nom":"Gazole","valeur":1.77,"maj":"2026-08-18T06:00:00Z"}]},
   "geometry":{"type":"Point","coordinates":[-4.486,48.39]}}]}`

func TestParseJSONNewFormat(t *testing.T) {
	st, err := parseJSON([]byte(newFormatJSON))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("expected 1 station, got %d", len(st))
	}
	s := st[0]
	if s.ID != "A1" || s.Ville != "Nantes" || s.CP != "44000" {
		t.Errorf("bad station: %+v", s)
	}
	if len(s.Fuels) != 2 {
		t.Fatalf("expected 2 fuels, got %d", len(s.Fuels))
	}
	if s.Fuels[0].Nom != "Gazole" || s.Fuels[0].Prix != 1.799 {
		t.Errorf("bad gazole: %+v", s.Fuels[0])
	}
	if s.Fuels[1].Prix != 1.899 { // chaîne "1.899" -> float
		t.Errorf("SP95 string not parsed: %+v", s.Fuels[1])
	}
}

func TestParseJSONLegacyFlat(t *testing.T) {
	st, err := parseJSON([]byte(legacyJSON))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("expected 1 station, got %d", len(st))
	}
	s := st[0]
	fuels := map[string]float64{}
	for _, f := range s.Fuels {
		fuels[f.Nom] = f.Prix
	}
	if fuels["Gazole"] != 1.85 || fuels["SP95"] != 1.95 {
		t.Errorf("legacy flat not reconstructed: %v", fuels)
	}
}

func TestParseJSONWrapped(t *testing.T) {
	st, err := parseJSON([]byte(wrappedJSON))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 || st[0].ID != "A3" {
		t.Fatalf("wrapped not parsed: %+v", st)
	}
	if st[0].Fuels[0].Prix != 1.78 {
		t.Errorf("wrapper fuel: %+v", st[0].Fuels)
	}
}

func TestParseGeoJSON(t *testing.T) {
	st, err := parseJSON([]byte(geojsonJSON))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 || st[0].ID != "G1" {
		t.Fatalf("geojson not parsed: %+v", st)
	}
	if st[0].Latitude != 48.39 || st[0].Longitude != -4.486 {
		t.Errorf("geojson coords: %f,%f", st[0].Latitude, st[0].Longitude)
	}
}

// TestParseJSONFluxInstantané reproduit le format réel du flux officiel
// « Flux instantané v2 » de data.economie.gouv.fr : latitude/longitude en
// Lambert-93 (mètres, inutilisables) et vraies coordonnées dans geom, id au
// format nombre, prix en colonnes plates.
func TestParseJSONFluxInstantane(t *testing.T) {
	data := `[
  {"id":80570001,"latitude":"5004475","longitude":"152562","cp":"80570","adresse":"Rue Joliot Curie",
   "ville":"Dargnies","geom":{"lon":1.52562,"lat":50.04475},
   "gazole_prix":"2.185","gazole_maj":"2026-07-29T12:19:55+00:00",
   "sp95_prix":"2.065","sp95_maj":"2026-07-29T12:19:55+00:00"}]`

	st, err := parseJSON([]byte(data))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("expected 1 station, got %d", len(st))
	}
	s := st[0]
	if s.ID != "80570001" {
		t.Errorf("id numérique mal décodé : %q", s.ID)
	}
	// Le flux officiel nomme la colonne postale "cp" (et non "code_postal").
	if s.CP != "80570" {
		t.Errorf("code postal non lu : %q", s.CP)
	}
	// Les coordonnées doivent venir de geom (degrés), PAS du Lambert.
	if s.Latitude != 50.04475 || s.Longitude != 1.52562 {
		t.Errorf("coordonnées attendues depuis geom : got %f,%f", s.Latitude, s.Longitude)
	}
	fuels := map[string]float64{}
	for _, f := range s.Fuels {
		fuels[f.Nom] = f.Prix
	}
	if fuels["Gazole"] != 2.185 || fuels["SP95"] != 2.065 {
		t.Errorf("prix plats non lus : %v", fuels)
	}
	if s.Fuels[0].Maj != "2026-07-29T12:19:55+00:00" {
		t.Errorf("maj : %q", s.Fuels[0].Maj)
	}
}

// TestNormalizeFuel couvre la normalisation des noms.
func TestNormalizeFuel(t *testing.T) {
	cases := map[string]string{
		"Gazole":          "Gazole",
		"gazole":          "Gazole",
		"SP95":            "SP95",
		"sp95-e5":         "SP95",
		"SP98":            "SP98",
		"E10":             "E10",
		"E85":             "E85",
		"Superethanol-E85": "E85",
		"superéthanol-e85": "E85",
		"GPLc":            "GPLc",
		"gpl-c":           "GPLc",
	}
	for in, want := range cases {
		if got := db.NormalizeFuel(in); got != want {
			t.Errorf("NormalizeFuel(%q) = %q, want %q", in, got, want)
		}
	}
}
