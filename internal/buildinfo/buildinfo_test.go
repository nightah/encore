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

func setBuildInfo(t *testing.T, bi *debug.BuildInfo, ok bool) {
	t.Helper()

	old := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return bi, ok }

	t.Cleanup(func() { readBuildInfo = old })
}

func vcsStamp(revision, at string) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "-trimpath", Value: "true"},
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.time", Value: at},
		{Key: "vcs.modified", Value: "false"},
	}}
}

func TestGetFallsBackToTheVCSStampWithoutALinkerCommit(t *testing.T) {
	setLinkerValues(t, "dev", "", "")
	setBuildInfo(t, vcsStamp("fedcba9876543210fedcba9876543210fedcba98", "2026-10-27T22:00:00Z"), true)

	assert.Equal(t, Info{
		Version:   "dev",
		Commit:    "fedcba9876543210fedcba9876543210fedcba98",
		Date:      "2026-10-27T22:00:00Z",
		GoVersion: runtime.Version(),
	}, Get())
}

func TestGetPrefersTheLinkerCommitOverTheVCSStamp(t *testing.T) {
	setLinkerValues(t, "v1.2.3", "0123456789abcdef", "2026-10-28")
	setBuildInfo(t, vcsStamp("fedcba9876543210", "2026-10-27T22:00:00Z"), true)

	got := Get()
	assert.Equal(t, "0123456789abcdef", got.Commit)
	assert.Equal(t, "2026-10-28", got.Date, "the date comes from the same source as the commit")
}

func TestGetWithoutAnyStamp(t *testing.T) {
	setLinkerValues(t, "dev", "", "2026-10-28")

	testCases := []struct {
		name string
		have *debug.BuildInfo
	}{
		{name: "ShouldKeepLinkerDateWithoutBuildInfo"},
		{name: "ShouldKeepLinkerDateWithoutVCSSettings", have: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "-trimpath", Value: "true"}}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setBuildInfo(t, tc.have, tc.have != nil)

			assert.Equal(t, Info{Version: "dev", Date: "2026-10-28", GoVersion: runtime.Version()}, Get(), "the linker date is kept when there is nothing to replace it")
		})
	}
}

func TestInfoString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		have     Info
		expected string
	}{
		{name: "ShouldShowNoCommit", have: Info{Version: "dev", GoVersion: "go1.27"}, expected: "encore dev (go1.27)"},
		{name: "ShouldShortenFullCommit", have: Info{Version: "v1.0.0", Commit: "0123456789abcdef0123", GoVersion: "go1.27"}, expected: "encore v1.0.0 (0123456789ab, go1.27)"},
		{name: "ShouldKeepTwelveCharacterCommitWhole", have: Info{Version: "v1.0.0", Commit: "0123456789ab", GoVersion: "go1.27"}, expected: "encore v1.0.0 (0123456789ab, go1.27)"},
		{name: "ShouldShortenThirteenCharacterCommit", have: Info{Version: "v1.0.0", Commit: "0123456789abc", GoVersion: "go1.27"}, expected: "encore v1.0.0 (0123456789ab, go1.27)"},
		{name: "ShouldKeepShortCommit", have: Info{Version: "v1.0.0", Commit: "abc", GoVersion: "go1.27"}, expected: "encore v1.0.0 (abc, go1.27)"},
		{name: "ShouldNotShowDate", have: Info{Version: "v1.0.0", Date: "2026-10-28", GoVersion: "go1.27"}, expected: "encore v1.0.0 (go1.27)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.String())
		})
	}
}
