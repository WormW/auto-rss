//go:build !embed

package webui

import (
	"io/fs"
	"os"
)

// DistFS returns a filesystem rooted at the built frontend output.
func DistFS() (fs.FS, error) {
	return os.DirFS("web/dist"), nil
}
