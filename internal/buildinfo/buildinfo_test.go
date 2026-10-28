package buildinfo

import (
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setLinkerValues(t *testing.T, version, commit, date string) {
	t.Helper()

	oldVersion, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = version, commit, date

	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })
}

func TestGetUsesTheLinkerValues(t *testing.T) {
	setLinkerValues(t, "v1.2.3", "0123456789abcdef0123456789abcdef01234567", "2026-10-28T09:00:00+11:00")

	assert.Equal(t, Info{
		Version:   "v1.2.3",
		Commit:    "0123456789abcdef0123456789abcdef01234567",
		Date:      "2026-10-28T09:00:00+11:00",
		GoVersion: runtime.Version(),
	}, Get())
}

func TestGetFallsBackToTheVCSStampWithoutALinkerCommit(t *testing.T) {
	setLinkerValues(t, "dev", "", "2026-01-01")

	var wantCommit, wantDate string

	wantDate = "2026-01-01"

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				wantCommit = s.Value
			case "vcs.time":
				wantDate = s.Value
			}
		}
	}

	got := Get()
	assert.Equal(t, "dev", got.Version)
	assert.Equal(t, wantCommit, got.Commit)
	assert.Equal(t, wantDate, got.Date)
	assert.Equal(t, runtime.Version(), got.GoVersion)
}

func TestInfoString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		info Info
		want string
	}{
		{"no commit", Info{Version: "dev", GoVersion: "go1.27"}, "encore dev (go1.27)"},
		{"full commit is shortened", Info{Version: "v1.0.0", Commit: "0123456789abcdef0123", GoVersion: "go1.27"}, "encore v1.0.0 (0123456789ab, go1.27)"},
		{"twelve characters kept whole", Info{Version: "v1.0.0", Commit: "0123456789ab", GoVersion: "go1.27"}, "encore v1.0.0 (0123456789ab, go1.27)"},
		{"thirteen characters shortened", Info{Version: "v1.0.0", Commit: "0123456789abc", GoVersion: "go1.27"}, "encore v1.0.0 (0123456789ab, go1.27)"},
		{"short commit kept", Info{Version: "v1.0.0", Commit: "abc", GoVersion: "go1.27"}, "encore v1.0.0 (abc, go1.27)"},
		{"date is not shown", Info{Version: "v1.0.0", Date: "2026-10-28", GoVersion: "go1.27"}, "encore v1.0.0 (go1.27)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.info.String())
		})
	}
}
