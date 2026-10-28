package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

const parseKeyError = "key must be 32 bytes encoded as standard base64"

func TestKeyEncodingRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		k := rapid.SliceOfN(rapid.Byte(), KeySize, KeySize).Draw(t, "key")

		got, err := ParseKey(EncodeKey(k))
		require.NoError(t, err)
		assert.Equal(t, k, got)
		assert.Equal(t, KeyID(k), KeyID(got), "the key ID is stable")
		assert.Regexp(t, `^v1-[0-9a-f]{16}$`, KeyID(k))
	})
}

func FuzzParseKey(f *testing.F) {
	k := make([]byte, KeySize)
	for _, seed := range []string{"", EncodeKey(k), " " + EncodeKey(k) + "\n", EncodeKey(k[:31]), EncodeKey(append(k, 0)), "!!!!", "c3VwZXJzZWNyZXQ="} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseKey(s)
		if err != nil {
			require.EqualError(t, err, parseKeyError, "the error never echoes the input")
			assert.Nil(t, got)

			return
		}

		require.Len(t, got, KeySize)

		again, err := ParseKey(EncodeKey(got))
		require.NoError(t, err)
		assert.Equal(t, got, again)
	})
}

func TestWriteKeyFile(t *testing.T) {
	k := make([]byte, KeySize)
	k[0] = 7

	path := filepath.Join(t.TempDir(), "nested", "dir", KeyFileName)
	require.NoError(t, writeKeyFile(path, k))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "missing directories are created private")

	got, err := readKeyFile(path)
	require.NoError(t, err)
	assert.Equal(t, k, got)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.ErrorContains(t, writeKeyFile(filepath.Join(file, KeyFileName), k), "creating key directory")

	// Renaming over a non-empty directory fails, and the temporary file is removed.
	dir := t.TempDir()
	target := filepath.Join(dir, KeyFileName)
	require.NoError(t, os.MkdirAll(filepath.Join(target, "x"), 0o700))
	require.ErrorContains(t, writeKeyFile(target, k), "writing key file")

	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWriteKeyFileUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	require.ErrorContains(t, writeKeyFile(filepath.Join(dir, KeyFileName), make([]byte, KeySize)), "creating key file")
}

func TestReadKeyFileErrors(t *testing.T) {
	_, err := readKeyFile(filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, os.ErrNotExist)

	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte("hunter2-not-a-key"), 0o600))

	_, err = readKeyFile(path)
	require.ErrorContains(t, err, path, "the error names the file")
	assert.NotContains(t, err.Error(), "hunter2", "but not its content")
}
