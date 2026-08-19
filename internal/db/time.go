package db

import "time"

// layouts acceptés par parseTimeFlexible, par ordre de tentative.
var timeLayouts = []string{
	time.RFC3339Nano,            // 2026-08-18T06:00:00.123Z
	time.RFC3339,                // 2026-08-18T06:00:00Z / +02:00
	"2006-01-02T15:04:05",       // naïf local (supposé UTC)
	"2006-01-02 15:04:05",       // format CSV
	"2006-01-02T15:04",          // sans secondes
	"2006-01-02",                // simple date
}

// parseTimeFlexible convertit un horodatage au format variable (nullable)
// vers time.Time UTC. Retourne une valeur zero si aucun layout ne matche.
func parseTimeFlexible(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	// Dernier repli : essayer de retirer des millisecondes non standard.
	for _, layout := range []string{"2006-01-02T15:04:05.999Z", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
