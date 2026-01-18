//go:build embed

package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var embeddedDist embed.FS

// DistFS returns a filesystem rooted at the embedded frontend output.
func DistFS() (fs.FS, error) {
	return fs.Sub(embeddedDist, "dist")
}
