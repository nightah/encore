package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"

	"github.com/nightah/encore/internal/config"
)

var _ Store = (*S3)(nil)

// NewS3 returns a store for cfg. It does not contact the server; use Probe for that.
func NewS3(cfg config.StorageS3) (*S3, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("s3: endpoint must be an http or https URL")
	}

	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("s3: endpoint %q must not have a path; use the bucket and prefix settings", u.Redacted())
	}

	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket must not be empty")
	}

	prefix, err := cleanPrefix(cfg.Prefix)
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}

	secure := u.Scheme == "https"

	transport, err := minio.DefaultTransport(secure)
	if err != nil {
		return nil, fmt.Errorf("s3: creating transport: %w", err)
	}

	if secure {
		if transport.TLSClientConfig, err = tlsConfig(cfg.CABundle, cfg.InsecureSkipVerify); err != nil {
			return nil, fmt.Errorf("s3: %w", err)
		}
	}

	lookup := minio.BucketLookupAuto

	switch cfg.Addressing {
	case config.AddressingPath:
		lookup = minio.BucketLookupPath
	case config.AddressingVirtual:
		lookup = minio.BucketLookupDNS
	}

	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       secure,
		Transport:    transport,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: creating client: %w", err)
	}

	s := &S3{client: client, bucket: cfg.Bucket, prefix: prefix}

	switch cfg.SSE {
	case config.SSES3:
		s.sse = encrypt.NewSSE()
	case config.SSEKMS:
		// Without an encryption context there is nothing to marshal, so NewSSEKMS can't fail.
		s.sse, _ = encrypt.NewSSEKMS(cfg.SSEKMSKeyID, nil)
	}

	return s, nil
}

func (s *S3) objectName(key string) string { return joinPrefix(s.prefix, key) }

// Put implements Store.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	_, err := s.client.PutObject(ctx, s.bucket, s.objectName(key), r, size, minio.PutObjectOptions{
		ContentType:          "application/octet-stream",
		ServerSideEncryption: s.sse,
	})
	if err != nil {
		return fmt.Errorf("uploading %s: %w", key, describeS3(err))
	}

	return nil
}

// Get implements Store.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	obj, err := s.client.GetObject(ctx, s.bucket, s.objectName(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, describeS3(err))
	}

	// GetObject is lazy; Stat issues the request so a missing key is reported here rather than on first Read.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()

		if isS3NotFound(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}

		return nil, fmt.Errorf("downloading %s: %w", key, describeS3(err))
	}

	return obj, nil
}

// Delete implements Store.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	if err := s.client.RemoveObject(ctx, s.bucket, s.objectName(key), minio.RemoveObjectOptions{}); err != nil && !isS3NotFound(err) {
		return fmt.Errorf("deleting %s: %w", key, describeS3(err))
	}

	return nil
}

// List implements Store.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}

	var out []Object

	for info := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.objectName(prefix), Recursive: true}) {
		if info.Err != nil {
			return nil, fmt.Errorf("listing %q: %w", prefix, describeS3(info.Err))
		}

		key := info.Key
		if s.prefix != "" {
			key = strings.TrimPrefix(key, s.prefix+"/")
		}

		out = append(out, Object{Key: key, Size: info.Size, Modified: info.LastModified.UTC()})
	}

	slices.SortFunc(out, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })

	return out, nil
}

// Probe implements Store. It checks the bucket exists first so a typo is reported as such rather than as a failed write.
func (s *S3) Probe(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("probe: checking bucket %q at %s: %w", s.bucket, s.client.EndpointURL().Redacted(), describeS3(err))
	}

	if !ok {
		return fmt.Errorf("probe: bucket %q does not exist at %s", s.bucket, s.client.EndpointURL().Redacted())
	}

	return probe(ctx, s)
}

func isS3NotFound(err error) bool {
	resp := minio.ToErrorResponse(err)

	return resp.Code == "NoSuchKey" || (resp.StatusCode == http.StatusNotFound && resp.Code != "NoSuchBucket")
}

// describeS3 adds the S3 error code and a hint for the mistakes people make most when pointing at a self-hosted server.
func describeS3(err error) error {
	resp := minio.ToErrorResponse(err)

	switch resp.Code {
	case "":
		return err
	case "NoSuchBucket":
		return fmt.Errorf("%w (the bucket does not exist; create it first)", err)
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return fmt.Errorf("%w (%s: check the access key, secret key and region)", err, resp.Code)
	case "AuthorizationHeaderMalformed", "InvalidRegion":
		return fmt.Errorf("%w (%s: set storage.s3.region to the server's region)", err, resp.Code)
	default:
		return fmt.Errorf("%w (%s)", err, resp.Code)
	}
}
