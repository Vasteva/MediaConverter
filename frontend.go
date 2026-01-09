package vastiva

import "embed"

//go:embed all:web/dist
var StaticFS embed.FS
