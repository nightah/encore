//go:build contract

package blobstore_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/blobstore/blobstoretest"
	"github.com/nightah/encore/internal/config"
)

// TestWebDAVServerContract runs the contract suite against a real WebDAV server (rclone and Nextcloud in CI). It is skipped unless ENCORE_CONTRACT_WEBDAV_URL is set.
func TestWebDAVServerContract(t *testing.T) {
	rawURL := os.Getenv("ENCORE_CONTRACT_WEBDAV_URL")
	if rawURL == "" {
		t.Skip("ENCORE_CONTRACT_WEBDAV_URL not set")
	}

	cfg := config.StorageWebDAV{
		URL:      rawURL,
		Username: os.Getenv("ENCORE_CONTRACT_WEBDAV_USERNAME"),
		Password: os.Getenv("ENCORE_CONTRACT_WEBDAV_PASSWORD"),
	}

	newStore := func(t *testing.T) blobstore.Store {
		t.Helper()

		c := cfg
		// A fresh prefix per subtest keeps them independent within one server.
		c.Prefix = "contract/" + randomName(t)

		s, err := blobstore.NewWebDAV(c)
		require.NoError(t, err)

		t.Cleanup(func() { cleanup(t, s) })

		return s
	}

	blobstoretest.Run(t, newStore)

	// A server that mishandles the request body can answer 201 and store nothing, so check what lands for both known and unknown sizes.
	t.Run("body stored intact", func(t *testing.T) {
		s := newStore(t)

		data := make([]byte, 1<<20)
		_, _ = rand.Read(data)

		for key, size := range map[string]int64{"known.bin": int64(len(data)), "unknown.bin": -1} {
			require.NoError(t, s.Put(t.Context(), key, io.MultiReader(bytes.NewReader(data)), size), key)

			objs, err := s.List(t.Context(), key)
			require.NoError(t, err)
			require.Len(t, objs, 1, key)
			assert.EqualValues(t, len(data), objs[0].Size, key)

			rc, err := s.Get(t.Context(), key)
			require.NoError(t, err)

			got, err := io.ReadAll(rc)
			_ = rc.Close()

			require.NoError(t, err)
			assert.True(t, bytes.Equal(data, got), "%s round trip", key)
		}
	})

	t.Run("bad credentials", func(t *testing.T) {
		if cfg.Username == "" {
			t.Skip("server has no credentials")
		}

		c := cfg
		c.Password = "wrong-password"

		s, err := blobstore.NewWebDAV(c)
		require.NoError(t, err)
		require.ErrorContains(t, s.Probe(t.Context()), "check the username and app password")
	})
}
