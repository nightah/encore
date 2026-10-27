//go:build contract

package blobstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/blobstore/blobstoretest"
	"github.com/nightah/encore/internal/config"
)

// TestS3Contract runs the contract suite against a real S3-compatible server (MinIO in CI). It is skipped unless ENCORE_CONTRACT_S3_ENDPOINT is set, and creates the bucket if needed.
func TestS3Contract(t *testing.T) {
	endpoint := os.Getenv("ENCORE_CONTRACT_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("ENCORE_CONTRACT_S3_ENDPOINT not set")
	}

	cfg := config.StorageS3{
		Endpoint:   endpoint,
		Region:     envOr("ENCORE_CONTRACT_S3_REGION", "us-east-1"),
		Bucket:     envOr("ENCORE_CONTRACT_S3_BUCKET", "encore-contract"),
		AccessKey:  envOr("ENCORE_CONTRACT_S3_ACCESS_KEY", "minioadmin"),
		SecretKey:  envOr("ENCORE_CONTRACT_S3_SECRET_KEY", "minioadmin"),
		Addressing: config.AddressingPath,
	}

	createBucket(t, cfg)

	for _, addressing := range []string{config.AddressingPath, config.AddressingAuto} {
		t.Run(addressing, func(t *testing.T) {
			blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
				c := cfg
				c.Addressing = addressing
				// A fresh prefix per subtest keeps them independent within one bucket.
				c.Prefix = "contract/" + randomName(t)

				s, err := blobstore.NewS3(c)
				require.NoError(t, err)

				t.Cleanup(func() { cleanup(t, s) })

				return s
			})
		})
	}

	t.Run("missing bucket", func(t *testing.T) {
		c := cfg
		c.Bucket = "encore-missing-" + randomName(t)

		s, err := blobstore.NewS3(c)
		require.NoError(t, err)
		require.ErrorContains(t, s.Probe(t.Context()), "does not exist")
	})

	t.Run("bad credentials", func(t *testing.T) {
		c := cfg
		c.SecretKey = "wrong-secret"

		s, err := blobstore.NewS3(c)
		require.NoError(t, err)
		require.ErrorContains(t, s.Probe(t.Context()), "check the access key")
	})
}

func createBucket(t *testing.T, cfg config.StorageS3) {
	t.Helper()

	s, err := blobstore.NewS3(cfg)
	require.NoError(t, err)

	// Use a raw client for admin work the Store interface deliberately does not offer.
	u := mustParseURL(t, cfg.Endpoint)
	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: minio.BucketLookupPath,
	})
	require.NoError(t, err)

	exists, err := client.BucketExists(t.Context(), cfg.Bucket)
	require.NoError(t, err)

	if !exists {
		require.NoError(t, client.MakeBucket(t.Context(), cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}))
	}

	require.NoError(t, s.Probe(t.Context()))
}

func cleanup(t *testing.T, s blobstore.Store) {
	t.Helper()

	// t.Context is already cancelled when cleanups run.
	ctx := context.Background()

	objs, err := s.List(ctx, "")
	if err != nil {
		return
	}

	for _, o := range objs {
		_ = s.Delete(ctx, o.Key)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	require.NoError(t, err)

	return u
}

func randomName(t *testing.T) string {
	t.Helper()

	var b [6]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)

	return hex.EncodeToString(b[:])
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
