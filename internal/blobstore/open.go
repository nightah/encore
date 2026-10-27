package blobstore

import (
	"fmt"
	"path/filepath"

	"github.com/nightah/encore/internal/config"
)

// Open returns the store selected by cfg.Backend. The local backend defaults to dataDir/blobs. No network calls are made; call Probe to check connectivity.
func Open(cfg config.Storage, dataDir string) (Store, error) {
	switch cfg.Backend {
	case config.BackendLocal, "":
		dir := cfg.Local.Path
		if dir == "" {
			dir = filepath.Join(dataDir, LocalDirName)
		}

		return NewLocal(dir)
	case config.BackendS3:
		return NewS3(cfg.S3)
	case config.BackendWebDAV:
		return NewWebDAV(cfg.WebDAV)
	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.Backend)
	}
}
