package morgue

import "embed"

//go:embed all:frontend/dist
var Assets embed.FS
