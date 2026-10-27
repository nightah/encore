//go:build !noui

package server

import (
	"embed"
	"io/fs"
)

// dist holds the Vite build output; web/ builds into it. The committed .gitkeep keeps the embed valid before the frontend has been built.
//
//go:embed all:dist
var dist embed.FS

func uiFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is a fixed, embedded directory
	}

	return sub
}
