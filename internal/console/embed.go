package console

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var distFS embed.FS

// Dist returns the embedded frontend filesystem.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "static")
}
