package blobstore_test

import (
	"bytes"
	"compress/gzip"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
)

func TestSnapshotErrors(t *testing.T) {
	s, err := blobstore.NewLocal(filepath.Join(t.TempDir(), "blobs"))
	require.NoError(t, err)

	ctx := t.Context()

	require.ErrorContains(t, blobstore.SaveJSON(ctx, s, "bad.json.gz", make(chan int)), "encoding bad.json.gz")

	objs, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, objs, "nothing is written when encoding fails")

	require.ErrorIs(t, blobstore.SaveJSON(ctx, s, "../escape.json.gz", 1), blobstore.ErrInvalidKey)

	require.NoError(t, s.Put(ctx, "plain.json.gz", bytes.NewReader([]byte(`{"a":1}`)), 7))

	var v map[string]int
	require.ErrorContains(t, blobstore.LoadJSON(ctx, s, "plain.json.gz", &v), "decompressing plain.json.gz")

	var gz bytes.Buffer

	zw := gzip.NewWriter(&gz)
	_, err = zw.Write([]byte(`{"a":`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, s.Put(ctx, "truncated.json.gz", &gz, int64(gz.Len())))

	require.ErrorContains(t, blobstore.LoadJSON(ctx, s, "truncated.json.gz", &v), "decoding truncated.json.gz")
}
