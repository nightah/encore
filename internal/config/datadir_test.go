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

	testCases := []struct {
		name     string
		goos     string
		env      map[string]string
		home     func() (string, error)
		expected string
	}{
		{name: "ShouldUseLocalShareOnLinux", goos: "linux", home: home, expected: filepath.Join(homeDir, ".local", "share", "encore")},
		{name: "ShouldUseXDGDataHomeOnLinux", goos: "linux", env: map[string]string{"XDG_DATA_HOME": xdg}, home: home, expected: filepath.Join(xdg, "encore")},
		{name: "ShouldIgnoreARelativeXDGDataHome", goos: "freebsd", env: map[string]string{"XDG_DATA_HOME": "relative"}, home: home, expected: filepath.Join(homeDir, ".local", "share", "encore")},
		{name: "ShouldUseApplicationSupportOnDarwin", goos: "darwin", home: home, expected: filepath.Join(homeDir, "Library", "Application Support", "encore")},
		{name: "ShouldUseLocalAppDataOnWindows", goos: "windows", env: map[string]string{"LocalAppData": local}, home: home, expected: filepath.Join(local, "encore")},
		{name: "ShouldFallBackWithoutAHome", goos: "linux", home: noHome, expected: fallbackDataDir},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, defaultDataDir(tc.goos, env(tc.env), tc.home))
		})
	}
}
