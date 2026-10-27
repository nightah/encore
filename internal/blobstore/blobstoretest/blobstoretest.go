package blobstoretest

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
)

// Run runs the contract suite. newStore must return an empty store; it is called once per subtest.
func Run(t *testing.T, newStore func(t *testing.T) blobstore.Store) {
	t.Helper()

	tests := []struct {
		name string
		fn   func(t *testing.T, s blobstore.Store)
	}{
		{"PutGet", testPutGet},
		{"Overwrite", testOverwrite},
		{"UnknownSize", testUnknownSize},
		{"Large", testLarge},
		{"GetMissing", testGetMissing},
		{"Delete", testDelete},
		{"List", testList},
		{"AwkwardKeys", testAwkwardKeys},
		{"InvalidKeys", testInvalidKeys},
		{"Cancelled", testCancelled},
		{"Probe", testProbe},
		{"JSONSnapshot", testJSONSnapshot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, newStore(t))
		})
	}
}

func put(t *testing.T, s blobstore.Store, key, body string) {
	t.Helper()
	require.NoError(t, s.Put(t.Context(), key, strings.NewReader(body), int64(len(body))))
}

func get(t *testing.T, s blobstore.Store, key string) string {
	t.Helper()

	rc, err := s.Get(t.Context(), key)
	require.NoError(t, err)

	defer rc.Close()

	b, err := io.ReadAll(rc)
	require.NoError(t, err)

	return string(b)
}

func keys(objs []blobstore.Object) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Key)
	}

	return out
}

func testPutGet(t *testing.T, s blobstore.Store) {
	put(t, s, "a/b/c.txt", "hello")
	assert.Equal(t, "hello", get(t, s, "a/b/c.txt"))

	put(t, s, "empty", "")
	assert.Empty(t, get(t, s, "empty"))
}

func testOverwrite(t *testing.T, s blobstore.Store) {
	put(t, s, "k", "first version")
	put(t, s, "k", "second")
	assert.Equal(t, "second", get(t, s, "k"))
}

func testUnknownSize(t *testing.T, s blobstore.Store) {
	require.NoError(t, s.Put(t.Context(), "unknown/size", strings.NewReader("streamed"), -1))
	assert.Equal(t, "streamed", get(t, s, "unknown/size"))
}

func testLarge(t *testing.T, s blobstore.Store) {
	data := make([]byte, 3<<20)
	_, _ = rand.Read(data)

	require.NoError(t, s.Put(t.Context(), "large.bin", bytes.NewReader(data), int64(len(data))))

	rc, err := s.Get(t.Context(), "large.bin")
	require.NoError(t, err)

	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, got), "large object round trip")
}

func testGetMissing(t *testing.T, s blobstore.Store) {
	_, err := s.Get(t.Context(), "nope/missing.json")
	require.ErrorIs(t, err, blobstore.ErrNotFound)

	put(t, s, "dir/file", "x")
	_, err = s.Get(t.Context(), "dir")
	require.ErrorIs(t, err, blobstore.ErrNotFound, "a directory is not an object")
}

func testDelete(t *testing.T, s blobstore.Store) {
	put(t, s, "d/x", "x")
	require.NoError(t, s.Delete(t.Context(), "d/x"))

	_, err := s.Get(t.Context(), "d/x")
	require.ErrorIs(t, err, blobstore.ErrNotFound)

	require.NoError(t, s.Delete(t.Context(), "d/x"), "deleting a missing object is not an error")
	require.NoError(t, s.Delete(t.Context(), "never/existed"))
}

func testList(t *testing.T, s blobstore.Store) {
	empty, err := s.List(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, empty)

	for _, k := range []string{"backups/encore-2.db.gz", "backups/encore-1.db.gz", "backups/old/encore-0.db.gz", "backupsx/other", "snapshots/lib.json.gz", "top"} {
		put(t, s, k, k)
	}

	all, err := s.List(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"backups/encore-1.db.gz", "backups/encore-2.db.gz", "backups/old/encore-0.db.gz", "backupsx/other", "snapshots/lib.json.gz", "top"}, keys(all))

	dir, err := s.List(t.Context(), "backups/")
	require.NoError(t, err)
	assert.Equal(t, []string{"backups/encore-1.db.gz", "backups/encore-2.db.gz", "backups/old/encore-0.db.gz"}, keys(dir))

	partial, err := s.List(t.Context(), "backups/encore-")
	require.NoError(t, err)
	assert.Equal(t, []string{"backups/encore-1.db.gz", "backups/encore-2.db.gz"}, keys(partial))
	assert.EqualValues(t, len("backups/encore-1.db.gz"), partial[0].Size)
	assert.False(t, partial[0].Modified.IsZero())

	bare, err := s.List(t.Context(), "backups")
	require.NoError(t, err)
	assert.Len(t, bare, 4, "a prefix without a slash matches by string prefix")

	none, err := s.List(t.Context(), "missing/")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func testAwkwardKeys(t *testing.T, s blobstore.Store) {
	key := "with space/ünïcødé & co/file #1?.json"
	put(t, s, key, "awkward")
	assert.Equal(t, "awkward", get(t, s, key))

	objs, err := s.List(t.Context(), "with space/")
	require.NoError(t, err)
	assert.Equal(t, []string{key}, keys(objs))
}

func testInvalidKeys(t *testing.T, s blobstore.Store) {
	for _, k := range []string{"", "/abs", "../escape", "a/../../b", "a//b", "a/./b", "trailing/", `back\slash`} {
		err := s.Put(t.Context(), k, strings.NewReader("x"), 1)
		require.ErrorIs(t, err, blobstore.ErrInvalidKey, "Put %q", k)

		_, err = s.Get(t.Context(), k)
		require.ErrorIs(t, err, blobstore.ErrInvalidKey, "Get %q", k)

		require.ErrorIs(t, s.Delete(t.Context(), k), blobstore.ErrInvalidKey, "Delete %q", k)
	}

	_, err := s.List(t.Context(), "../")
	require.ErrorIs(t, err, blobstore.ErrInvalidKey)
}

func testCancelled(t *testing.T, s blobstore.Store) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.Error(t, s.Put(ctx, "cancelled", strings.NewReader("x"), 1))

	_, err := s.Get(t.Context(), "cancelled")
	require.ErrorIs(t, err, blobstore.ErrNotFound, "a cancelled Put leaves no object behind")
}

func testProbe(t *testing.T, s blobstore.Store) {
	require.NoError(t, s.Probe(t.Context()))

	objs, err := s.List(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, objs, "the probe object is cleaned up")
}

func testJSONSnapshot(t *testing.T, s blobstore.Store) {
	type snapshot struct {
		Titles []string `json:"titles"`
	}

	in := snapshot{Titles: []string{"Alien", "Aliens"}}
	require.NoError(t, blobstore.SaveJSON(t.Context(), s, "snapshots/library.json.gz", in))

	var out snapshot
	require.NoError(t, blobstore.LoadJSON(t.Context(), s, "snapshots/library.json.gz", &out))
	assert.Equal(t, in, out)

	require.ErrorIs(t, blobstore.LoadJSON(t.Context(), s, "snapshots/missing.json.gz", &out), blobstore.ErrNotFound)
}
