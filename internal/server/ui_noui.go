//go:build noui

package server

import "io/fs"

// uiFS returns nil in builds made with -tags noui, which serve a placeholder page instead of the web interface.
func uiFS() fs.FS {
	return nil
}
