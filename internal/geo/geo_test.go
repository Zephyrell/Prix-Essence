package geo

import (
	"math"
	"testing"
)

func assertClose(t *testing.T, got, want, tol float64, msg string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %f, want %f (±%f)", msg, got, want, tol)
	}
}

func TestHaversine(t *testing.T) {
	assertClose(t, Haversine(0, 0, 0, 1), 111.195, 2, "1° d'équateur")
	assertClose(t, Haversine(48.8566, 2.3522, 48.8566, 2.3522), 0, 0.001, "même point")
	assertClose(t, Haversine(48.8566, 2.3522, 45.7640, 4.8357), 392.0, 4, "Paris→Lyon")
}

func TestBoundingBox(t *testing.T) {
	minLat, maxLat, minLon, maxLon := BoundingBox(45.0, 5.0, 111.32)
	// latitude : ±1° pour 111.32 km
	if math.Abs(minLat-44.0) > 0.01 || math.Abs(maxLat-46.0) > 0.01 {
		t.Errorf("lat bbox: %f..%f", minLat, maxLat)
	}
	// longitude : cos(45°) ≈ 0.707 → ±1.414°
	if math.Abs(minLon-3.586) > 0.05 || math.Abs(maxLon-6.414) > 0.05 {
		t.Errorf("lon bbox: %f..%f", minLon, maxLon)
	}
}

func TestBoundingBoxCenterInside(t *testing.T) {
	minLat, maxLat, minLon, maxLon := BoundingBox(45.0, 5.0, 50)
	if !(minLat <= 45 && 45 <= maxLat && minLon <= 5 && 5 <= maxLon) {
		t.Error("le centre doit être dans la bbox")
	}
}
