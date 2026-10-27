package blobstore

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// tlsConfig trusts the system pool plus an optional PEM bundle, for servers behind a private CA.
func tlsConfig(caBundle string, insecureSkipVerify bool) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // opt-in for lab setups, documented as insecure
	}

	if caBundle == "" {
		return cfg, nil
	}

	pem, err := os.ReadFile(caBundle) //nolint:gosec // the operator chooses the CA bundle
	if err != nil {
		return nil, fmt.Errorf("reading CA bundle: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA bundle contains no PEM certificates")
	}

	cfg.RootCAs = pool

	return cfg, nil
}
