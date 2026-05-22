package web

import (
	"embed"
	"io/fs"
)

//go:embed assets
var rawFS embed.FS

// staticFS is the assets/ subtree, served under /static/.
var staticFS, _ = fs.Sub(rawFS, "assets")
