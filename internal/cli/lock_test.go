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
