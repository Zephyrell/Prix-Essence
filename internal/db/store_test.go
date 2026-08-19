package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func mustOpenTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var testTs = time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)

func TestCitiesAccentCaseInsensitive(t *testing.T) {
	s := mustOpenTest(t)
	ctx := context.Background()
	st := []Station{
		{ID: "A", Ville: "Paris", CP: "75010", Latitude: 48.87, Longitude: 2.36},
		{ID: "B", Ville: "Créteil", CP: "94000", Latitude: 48.78, Longitude: 2.45},
		{ID: "C", Ville: "Évry", CP: "91000", Latitude: 48.63, Longitude: 2.43},
		{ID: "D", Ville: "Damparis", CP: "39500", Latitude: 47.07, Longitude: 5.41},
		{ID: "E", Ville: "Bordeaux", CP: "33000", Latitude: 44.84, Longitude: -0.58},
	}
	if _, err := s.Import(ctx, st, testTs, "src", "lm"); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Casse insensible : "par" doit retourner Paris en premier (préfixe),
	// Damparis étant une sous-chaîne classée ensuite.
	got, err := s.Cities(ctx, "par", 10)
	if err != nil {
		t.Fatalf("cities: %v", err)
	}
	if len(got) == 0 || got[0].Ville != "Paris" {
		t.Fatalf("q=par first = %+v, want Paris en premier", got)
	}
	if got[0].CP != "75010" {
		t.Fatalf("q=par CP = %q, want 75010 (les CP vides sont défavorisés)", got[0].CP)
	}

	// Accent insensible : "creteil" doit trouver Créteil.
	if got, err := s.Cities(ctx, "creteil", 10); err != nil || len(got) == 0 || got[0].Ville != "Créteil" {
		t.Fatalf("q=creteil = %+v (err=%v), want Créteil", got, err)
	}

	// Accent dans la saisie : "Créteil" doit matcher lui-même.
	if got, err := s.Cities(ctx, "Créteil", 10); err != nil || len(got) == 0 || got[0].Ville != "Créteil" {
		t.Fatalf("q=Créteil = %+v (err=%v)", got, err)
	}

	// É majuscule accentuée dans la ville : "evry" doit trouver Évry.
	if got, err := s.Cities(ctx, "evry", 10); err != nil || len(got) == 0 || got[0].Ville != "Évry" {
		t.Fatalf("q=evry = %+v (err=%v), want Évry", got, err)
	}

	// Sens du tri : les préfixes exacts d'abord, puis les sous-chaînes.
	got, err = s.Cities(ctx, "b", 10)
	if err != nil {
		t.Fatalf("cities b: %v", err)
	}
	if len(got) != 1 || got[0].Ville != "Bordeaux" {
		t.Fatalf("q=b = %+v, want Bordeaux", got)
	}

	// Espace vs tiret vs apostrophe : "saint paul", "saint-paul" et
	// "saintpaul" doivent matcher Saint-Paul-Trois-Châteaux.
	st = append(st, Station{ID: "F", Ville: "Saint-Paul-Trois-Châteaux", CP: "26130",
		Latitude: 44.35, Longitude: 4.78})
	if _, err := s.Import(ctx, st, testTs, "src", "lm"); err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, q := range []string{"saint paul", "saint-paul", "saintpaul"} {
		got, err := s.Cities(ctx, q, 10)
		if err != nil || len(got) == 0 || got[0].Ville != "Saint-Paul-Trois-Châteaux" {
			t.Fatalf("q=%q = %+v (err=%v), want Saint-Paul-Trois-Châteaux", q, got, err)
		}
	}

	// Recherche par code postal : "26130" doit cibler Saint-Paul-Trois-Châteaux.
	got, err = s.Cities(ctx, "26130", 10)
	if err != nil || len(got) == 0 {
		t.Fatalf("q=26130 = %+v (err=%v)", got, err)
	}
	if got[0].Ville != "Saint-Paul-Trois-Châteaux" || got[0].CP != "26130" {
		t.Fatalf("recherche CP : %+v, want Saint-Paul-Trois-Châteaux 26130", got[0])
	}
}

func TestStoreImportHistory(t *testing.T) {
	s := mustOpenTest(t)
	ctx := context.Background()
	st := []Station{{
		ID: "A", Adresse: "x", Ville: "v", CP: "75000",
		Latitude: 48.85, Longitude: 2.35,
		Fuels: []Fuel{{Nom: "Gazole", Prix: 1.80, Maj: "2026-08-17T10:00:00Z"}},
	}}

	// 1) Premier import → 1 ligne d'historique.
	if _, err := s.Import(ctx, st, testTs, "u", "m"); err != nil {
		t.Fatalf("import 1: %v", err)
	}
	h, _ := s.History(ctx, "A", "Gazole", 14)
	if len(h) != 1 || h[0].Prix != 1.80 {
		t.Fatalf("history après import 1: %+v", h)
	}

	// 2) Re-import du même prix → toujours une seule ligne (pas d'empilement).
	if _, err := s.Import(ctx, st, testTs, "u", "m"); err != nil {
		t.Fatalf("import 2: %v", err)
	}
	h, _ = s.History(ctx, "A", "Gazole", 14)
	if len(h) != 1 {
		t.Fatalf("même prix devrait rester 1 ligne, got %d", len(h))
	}

	// 3) Changement de prix le même jour (maj 08-17 18:00) → ligne mise à jour.
	st[0].Fuels[0].Prix = 1.82
	st[0].Fuels[0].Maj = "2026-08-17T18:00:00Z"
	if _, err := s.Import(ctx, st, testTs, "u", "m"); err != nil {
		t.Fatalf("import 3: %v", err)
	}
	h, _ = s.History(ctx, "A", "Gazole", 14)
	if len(h) != 1 || h[0].Prix != 1.82 {
		t.Fatalf("même jour → mise à jour attendue, got %+v", h)
	}

	// 4) Changement le lendemain → 2 lignes.
	st[0].Fuels[0].Prix = 1.85
	st[0].Fuels[0].Maj = "2026-08-18T10:00:00Z"
	if _, err := s.Import(ctx, st, testTs, "u", "m"); err != nil {
		t.Fatalf("import 4: %v", err)
	}
	h, _ = s.History(ctx, "A", "Gazole", 14)
	if len(h) != 2 {
		t.Fatalf("2 jours attendus, got %d (%+v)", len(h), h)
	}
}

func TestStorePurge14Days(t *testing.T) {
	s := mustOpenTest(t)
	ctx := context.Background()

	// Insère une ligne d'historique vieille de plus de 14 jours.
	old := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	if _, err := s.db.Exec(`INSERT INTO historique (station_id, carburant, jour, prix, maj)
		VALUES ('A','Gazole',?,1.60,'x')`, old); err != nil {
		t.Fatalf("insert old: %v", err)
	}

	// Un import déclenche la purge.
	st := []Station{{ID: "A", Adresse: "x", Ville: "v", CP: "75000", Latitude: 48.85, Longitude: 2.35,
		Fuels: []Fuel{{Nom: "Gazole", Prix: 1.80, Maj: time.Now().Format(time.RFC3339)}}}}
	stats, err := s.Import(ctx, st, testTs, "u", "m")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.HistoryDeleted != 1 {
		t.Errorf("expected 1 purgé, got %d", stats.HistoryDeleted)
	}
}

func TestStationsInRadius(t *testing.T) {
	s := mustOpenTest(t)
	ctx := context.Background()
	stations := []Station{
		{ID: "N1", Adresse: "Près", Ville: "Paris", CP: "75000", Latitude: 48.8500, Longitude: 2.3500,
			Fuels: []Fuel{{Nom: "Gazole", Prix: 1.80, Maj: testTs.Format(time.RFC3339)}}},
		{ID: "N2", Adresse: "Loin", Ville: "Véry", CP: "75000", Latitude: 48.82, Longitude: 2.35,
			Fuels: []Fuel{{Nom: "Gazole", Prix: 1.75, Maj: testTs.Format(time.RFC3339)}}},
		// Sans prix renseigné → exclu.
		{ID: "N3", Adresse: "Sans prix", Ville: "Paris", CP: "75000", Latitude: 48.8510, Longitude: 2.3510,
			Fuels: []Fuel{{Nom: "Gazole", Prix: 0, Maj: ""}}},
	}
	if _, err := s.Import(ctx, stations, testTs, "u", "m"); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Rayon de 2 km autour de 48.8500,2.3500 → seule N1 (à ~0 km) est dedans.
	views, err := s.StationsInRadius(ctx, 48.8500, 2.3500, 2)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(views) != 1 || views[0].ID != "N1" {
		t.Fatalf("expected 1 (N1), got %+v", views)
	}
	if views[0].DistanceKm > 0.1 {
		t.Errorf("distance N1 anormale: %f", views[0].DistanceKm)
	}

	// Rayon 10 km → N1 et N2, triées par distance.
	views, err = s.StationsInRadius(ctx, 48.8500, 2.3500, 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2, got %d", len(views))
	}
	if views[0].ID != "N1" || views[1].ID != "N2" {
		t.Errorf("tri par distance attendu: %s puis %s", views[0].ID, views[1].ID)
	}
}

func TestAllStationsByFuelFilter(t *testing.T) {
	s := mustOpenTest(t)
	ctx := context.Background()
	stations := []Station{
		{ID: "F1", Adresse: "a", Ville: "Aix", CP: "13000", Latitude: 43.5, Longitude: 5.4,
			Fuels: []Fuel{{Nom: "Gazole", Prix: 1.80, Maj: testTs.Format(time.RFC3339)}}},
		{ID: "F2", Adresse: "b", Ville: "Brest", CP: "29200", Latitude: 48.4, Longitude: -4.5,
			Fuels: []Fuel{{Nom: "E85", Prix: 0.80, Maj: testTs.Format(time.RFC3339)}}},
	}
	if _, err := s.Import(ctx, stations, testTs, "u", "m"); err != nil {
		t.Fatalf("import: %v", err)
	}
	views, err := s.AllStationsByFuel(ctx, "Gazole", 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(views) != 1 || views[0].ID != "F1" {
		t.Fatalf("expected only F1 for Gazole, got %+v", views)
	}
}
