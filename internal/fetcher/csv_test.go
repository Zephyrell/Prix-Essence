package fetcher

import "testing"

// TestParseCSV couvre : BOM, délimiteur ';', adresse quotée avec virgule,
// virgule décimale dans les prix et coordonnées.
func TestParseCSV(t *testing.T) {
	data := "\xEF\xBB\xBFid;adresse;cp;ville;latitude;longitude;gazole_prix;gazole_maj\n" +
		"A1;\"1, rue de la Gare\";44000;Nantes;47,218;-1,553;1,799;2026-08-18 06:00:00\n" +
		"A2;2 Rue Y;69001;Lyon;45,764;4,836;1,850;2026-08-18 06:00:00\n"

	st, err := parseCSV([]byte(data))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 2 {
		t.Fatalf("expected 2 stations, got %d", len(st))
	}
	s := st[0]
	if s.ID != "A1" || s.Adresse != "1, rue de la Gare" {
		t.Errorf("station 1 bad: %+v", s)
	}
	if s.Latitude != 47.218 || s.Longitude != -1.553 {
		t.Errorf("coords: %f,%f", s.Latitude, s.Longitude)
	}
	if len(s.Fuels) != 1 || s.Fuels[0].Prix != 1.799 {
		t.Errorf("fuel gazole: %+v", s.Fuels)
	}
}

// TestParseCSVComma vérifie le détecteur de délimiteur ','.
func TestParseCSVComma(t *testing.T) {
	data := "id,adresse,ville,latitude,longitude,sp95_prix\n" +
		"B1,1 Rue X,Paris,48.85,2.35,1.92\n"
	st, err := parseCSV([]byte(data))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 || st[0].Fuels[0].Prix != 1.92 {
		t.Fatalf("comma CSV: %+v", st)
	}
}

// TestParseCSVLatin1 vérifie la transposition latin-1 → UTF-8.
func TestParseCSVLatin1(t *testing.T) {
	// "Sète" en latin-1 : S \xE8 te — il faut un prix pour que la station soit gardée.
	data := []byte("\xEF\xBB\xBFid;ville;gazole_prix\n" + "C1;S\xE8te;1,79\n")
	st, err := parseCSV([]byte(data))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(st) != 1 || st[0].Ville != "Sète" {
		t.Fatalf("latin1: %+v", st)
	}
	if st[0].Fuels[0].Prix != 1.79 {
		t.Errorf("prix latin1: %+v", st[0].Fuels)
	}
}
