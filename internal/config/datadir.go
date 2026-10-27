package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// DefaultDataDir returns where Encore keeps its data when data_dir isn't set: the platform's per-user data directory, so a plain `encore serve` works without root. The container image (/config) and the systemd unit (/var/lib/encore) override it.
func DefaultDataDir() string {
	return defaultDataDir(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func defaultDataDir(goos string, getenv func(string) string, home func() (string, error)) string {
	switch goos {
	case "windows":
		if dir := getenv("LocalAppData"); dir != "" {
			return filepath.Join(dir, "encore")
		}
	case "darwin", "ios":
		if h, err := home(); err == nil && h != "" {
			return filepath.Join(h, "Library", "Application Support", "encore")
		}
	default:
		// The XDG base directory spec ignores relative paths.
		if dir := getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
			return filepath.Join(dir, "encore")
		}

		if h, err := home(); err == nil && h != "" {
			return filepath.Join(h, ".local", "share", "encore")
		}
	}

	return fallbackDataDir
}
