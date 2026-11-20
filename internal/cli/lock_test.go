package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockDataDirCreatesTheDirectoryAndRecordsThePID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")

	lock, err := lockDataDir(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	b, err := os.ReadFile(filepath.Join(dir, lockFileName))
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid())+"\n", string(b), "the holder is recorded for the message a second Encore shows")

	_, err = lockDataDir(dir)
	require.ErrorIs(t, err, errLocked)
	assert.Contains(t, err.Error(), "another Encore (pid ")
	assert.Contains(t, err.Error(), dir)
}

func TestLockDataDirReportsAnUnusableLockFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, lockFileName), 0o700))

	_, err := lockDataDir(dir)
	require.Error(t, err)
	require.NotErrorIs(t, err, errLocked)
	assert.Contains(t, err.Error(), "opening lock file")
}

func TestLockDataDirNamesTheHolderOnlyWhenItsPIDIsReadable(t *testing.T) {
	dir := t.TempDir()

	lock, err := lockDataDir(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldNameThePID", have: "4242\n", expected: "another Encore (pid 4242) is using the data directory "},
		{name: "ShouldNameAPIDSurroundedBySpace", have: " 4242 \r\n", expected: "another Encore (pid 4242) is using the data directory "},
		{name: "ShouldNotNameAnEmptyFile", have: "", expected: "another Encore is using the data directory "},
		{name: "ShouldNotNameText", have: "encore\n", expected: "another Encore is using the data directory "},
		{name: "ShouldNotNameAPIDFollowedByText", have: "42abc\n", expected: "another Encore is using the data directory "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, lockFileName), []byte(tc.have), 0o600))

			_, err := lockDataDir(dir)
			require.ErrorIs(t, err, errLocked)
			assert.Contains(t, err.Error(), tc.expected+dir)
		})
	}
}
