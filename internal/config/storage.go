package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (s *Storage) validate() []error {
	var errs []error

	switch s.Backend {
	case BackendLocal:
	case BackendS3:
		errs = append(errs, s.S3.validate()...)
	case BackendWebDAV:
		errs = append(errs, s.WebDAV.validate()...)
	default:
		errs = append(errs, fmt.Errorf("storage.backend %q must be local, s3 or webdav", s.Backend))
	}

	if _, err := ParseSchedule(s.Backups.Schedule); err != nil {
		errs = append(errs, fmt.Errorf("storage.backups.schedule: %w", err))
	}

	if s.Backups.Keep < 1 {
		errs = append(errs, fmt.Errorf("storage.backups.keep %d must be at least 1", s.Backups.Keep))
	}

	return errs
}

func (s *StorageS3) validate() []error {
	var errs []error

	if err := validateHTTPURL(s.Endpoint); err != nil {
		errs = append(errs, fmt.Errorf("storage.s3.endpoint: %w", err))
	}

	if s.Bucket == "" {
		errs = append(errs, errors.New("storage.s3.bucket must not be empty"))
	}

	if s.AccessKey == "" || s.SecretKey == "" {
		errs = append(errs, errors.New("storage.s3.access_key and storage.s3.secret_key must both be set"))
	}

	switch s.Addressing {
	case AddressingAuto, AddressingPath, AddressingVirtual:
	default:
		errs = append(errs, fmt.Errorf("storage.s3.addressing %q must be auto, path or virtual", s.Addressing))
	}

	switch s.SSE {
	case SSENone, SSES3:
	case SSEKMS:
		if s.SSEKMSKeyID == "" {
			errs = append(errs, errors.New("storage.s3.sse_kms_key_id must be set when storage.s3.sse is kms"))
		}
	default:
		errs = append(errs, fmt.Errorf("storage.s3.sse %q must be empty, s3 or kms", s.SSE))
	}

	return errs
}

func (w *StorageWebDAV) validate() []error {
	if err := validateHTTPURL(w.URL); err != nil {
		return []error{fmt.Errorf("storage.webdav.url: %w", err)}
	}

	return nil
}

func (s *Secrets) validate() []error {
	if s.Key != "" && s.KeyFile != "" {
		return []error{errors.New("secrets.key and secrets.key_file are mutually exclusive")}
	}

	if s.Key != "" {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.Key)); err != nil || len(b) != 32 {
			// Never echo the value: it is key material.
			return []error{errors.New("secrets.key must be 32 bytes encoded as standard base64")}
		}
	}

	return nil
}

func validateHTTPURL(raw string) error {
	if raw == "" {
		return errors.New("must not be empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("is not a valid URL")
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q must be an http or https URL", u.Redacted())
	}

	return nil
}
