// Package assets embeds the compiled static asset tree (Tailwind CSS,
// any custom JS, fonts, favicons). Mirrors Triton's assets package so
// main.go and any future test suite serve identical bytes.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var root embed.FS

// Static returns the embedded asset tree rooted at "static/" — pass it
// straight to http.FileServer(http.FS(...)).
func Static() fs.FS {
	sub, err := fs.Sub(root, "static")
	if err != nil {
		panic("assets: " + err.Error())
	}
	return sub
}
