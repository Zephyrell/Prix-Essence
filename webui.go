// Package webui embarque les assets frontend via go:embed.
// Placé à la racine du module : les patterns go:embed ne peuvent pas contenir
// "../", donc un package situé au niveau de web/ est nécessaire.
package webui

import "embed"

//go:embed all:web
var FS embed.FS
