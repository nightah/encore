package config

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDataDir(t *testing.T) {
	const (
		homeDir = "/home/encore"
		xdg     = "/srv/xdg"
		local   = `C:\Users\encore\AppData\Local`
	)

	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	home := func() (string, error) { return homeDir, nil }
	noHome := func() (string, error) { return "", errors.New("no home") }

	assert.Equal(t, filepath.Join(homeDir, ".local", "share", "encore"), defaultDataDir("linux", env(nil), home))
	assert.Equal(t, filepath.Join(xdg, "encore"), defaultDataDir("linux", env(map[string]string{"XDG_DATA_HOME": xdg}), home))
	assert.Equal(t, filepath.Join(homeDir, ".local", "share", "encore"), defaultDataDir("freebsd", env(map[string]string{"XDG_DATA_HOME": "relative"}), home), "a relative XDG_DATA_HOME is ignored")
	assert.Equal(t, filepath.Join(homeDir, "Library", "Application Support", "encore"), defaultDataDir("darwin", env(nil), home))
	assert.Equal(t, filepath.Join(local, "encore"), defaultDataDir("windows", env(map[string]string{"LocalAppData": local}), home))
	assert.Equal(t, fallbackDataDir, defaultDataDir("linux", env(nil), noHome))
}
