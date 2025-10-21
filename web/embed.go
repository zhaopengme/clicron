package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js styles.css
var content embed.FS

// Files exposes the embedded static assets.
func Files() fs.FS {
	return content
}
