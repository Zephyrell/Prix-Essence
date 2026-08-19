// Package geo fournit des primitives de calcul géographique (haversine,
// bounding box) pour la recherche de stations autour d'un point.
package geo

import "math"

// EarthRadiusKm rayon moyen de la Terre en kilomètres.
const EarthRadiusKm = 6371.0

// Haversine retourne la distance à vol d'oiseau (km) entre deux points
// géographiques donnés en degrés décimaux.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	p1 := lat1 * math.Pi / 180
	p2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLam := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(p1)*math.Cos(p2)*math.Sin(dLam/2)*math.Sin(dLam/2)
	return 2 * EarthRadiusKm * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// BoundingBox retourne le rectangle (minLat, maxLat, minLon, maxLon) qui
// encadre un cercle de rayon radiusKm centré sur (lat, lon). Approximation
// locale suffisante pour un rayon < quelques centaines de km.
func BoundingBox(lat, lon, radiusKm float64) (minLat, maxLat, minLon, maxLon float64) {
	latD := radiusKm / 111.32                       // 1° de latitude ≈ 111,32 km
	lonD := radiusKm / (111.32 * math.Cos(lat*math.Pi/180))
	return lat - latD, lat + latD, lon - lonD, lon + lonD
}
