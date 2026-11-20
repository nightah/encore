package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 bytes

func TestStorageDefaults(t *testing.T) {
	cfg, err := Load(Options{Environ: environ()})
	require.NoError(t, err)

	assert.Equal(t, BackendLocal, cfg.Storage.Backend)
	assert.Equal(t, AddressingAuto, cfg.Storage.S3.Addressing)
	assert.Equal(t, "daily@03:00", cfg.Storage.Backups.Schedule)
	assert.Equal(t, 14, cfg.Storage.Backups.Keep)
	assert.Equal(t, "Encore", cfg.Storage.Backups.Prefix)
	assert.Empty(t, cfg.Secrets.Key)
}

func TestStorageS3FromEnv(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "s3_secret")
	require.NoError(t, os.WriteFile(secret, []byte("s3cr3t\n"), 0o600))

	cfg, err := Load(Options{Environ: environ(
		"ENCORE_STORAGE_BACKEND=s3",
		"ENCORE_STORAGE_S3_ENDPOINT=https://minio.lan:9000",
		"ENCORE_STORAGE_S3_BUCKET=encore",
		"ENCORE_STORAGE_S3_ADDRESSING=path",
		"ENCORE_STORAGE_S3_ACCESS_KEY=AKIA",
		"ENCORE_STORAGE_S3_SECRET_KEY_FILE="+secret,
		"ENCORE_STORAGE_S3_INSECURE_SKIP_VERIFY=true",
		"ENCORE_STORAGE_BACKUPS_KEEP=3",
	)})
	require.NoError(t, err)

	s3 := cfg.Storage.S3
	assert.Equal(t, "https://minio.lan:9000", s3.Endpoint)
	assert.Equal(t, "encore", s3.Bucket)
	assert.Equal(t, AddressingPath, s3.Addressing)
	assert.Equal(t, "s3cr3t", s3.SecretKey)
	assert.True(t, s3.InsecureSkipVerify)
	assert.Equal(t, 3, cfg.Storage.Backups.Keep)
}

func TestStorageWebDAVFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encore.yml")
	require.NoError(t, os.WriteFile(path, []byte(`storage:
  backend: webdav
  webdav:
    url: https://cloud.example.com/remote.php/dav/files/backup/encore
    username: backup
    password: app-password
`), 0o600))

	cfg, err := Load(Options{File: path, Environ: environ()})
	require.NoError(t, err)

	assert.Equal(t, "backup", cfg.Storage.WebDAV.Username)
	assert.Equal(t, "app-password", cfg.Storage.WebDAV.Password)
}

func TestStorageValidation(t *testing.T) {
	testCases := []struct {
		name     string
		mutate   func(*Config)
		expected []string
	}{
		{name: "ShouldRejectAnUnknownBackend", mutate: func(c *Config) { c.Storage.Backend = "ftp" }, expected: []string{"storage.backend"}},
		{
			name: "ShouldRejectAnIncompleteS3Backend",
			mutate: func(c *Config) {
				c.Storage.Backend = BackendS3
				c.Storage.S3.Endpoint = "minio:9000"
				c.Storage.S3.Addressing = "dns"
				c.Storage.S3.SSE = "aes"
			},
			expected: []string{"storage.s3.endpoint", "storage.s3.bucket", "storage.s3.access_key", "storage.s3.addressing", "storage.s3.sse"},
		},
		{
			name: "ShouldRejectS3KMSWithoutAKey",
			mutate: func(c *Config) {
				c.Storage.Backend = BackendS3
				c.Storage.S3 = StorageS3{Endpoint: "http://minio:9000", Bucket: "b", AccessKey: "a", SecretKey: "s", Addressing: AddressingAuto, SSE: SSEKMS}
			},
			expected: []string{"sse_kms_key_id"},
		},
		{name: "ShouldRejectWebDAVWithoutAURL", mutate: func(c *Config) { c.Storage.Backend = BackendWebDAV }, expected: []string{"storage.webdav.url"}},
		{
			name: "ShouldRejectAWebDAVURLWithABadScheme",
			mutate: func(c *Config) {
				c.Storage.Backend = BackendWebDAV
				c.Storage.WebDAV.URL = "ftp://user:hunter2@host/dav" // trufflehog:ignore
			},
			expected: []string{"storage.webdav.url"},
		},
		{
			name: "ShouldRejectBadBackupSettings",
			mutate: func(c *Config) {
				c.Storage.Backups.Schedule = "fortnightly"
				c.Storage.Backups.Keep = 0
			},
			expected: []string{"storage.backups.schedule", "storage.backups.keep"},
		},
		{
			name: "ShouldRejectBothASecretsKeyAndKeyFile",
			mutate: func(c *Config) {
				c.Secrets.Key = testKey
				c.Secrets.KeyFile = "/run/secrets/key"
			},
			expected: []string{"mutually exclusive"},
		},
		{name: "ShouldRejectAShortSecretsKey", mutate: func(c *Config) { c.Secrets.Key = "c2hvcnQ=" }, expected: []string{"secrets.key"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			tc.mutate(&cfg)

			err := cfg.Validate()
			require.Error(t, err)

			for _, expected := range tc.expected {
				assert.Contains(t, err.Error(), expected)
			}

			assert.NotContains(t, err.Error(), "hunter2", "credentials in URLs must be redacted")
		})
	}
}

func TestStorageInactiveBackendNotValidated(t *testing.T) {
	cfg := Defaults()
	cfg.Storage.S3.Addressing = "nonsense"
	cfg.Secrets.Key = testKey

	assert.NoError(t, cfg.Validate())
}

func TestSecretsKeyFileEnv(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(keyFile, []byte(testKey+"\n"), 0o600))

	cfg, err := Load(Options{Environ: environ("ENCORE_SECRETS_KEY_FILE=" + keyFile)})
	require.NoError(t, err)

	assert.Equal(t, testKey, cfg.Secrets.Key)
	assert.Empty(t, cfg.Secrets.KeyFile)
}
