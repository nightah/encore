package config

import "errors"

// In stateless mode the data directory is temporary, so the database is restored from and saved to the storage backend, and the secrets key must come from config rather than a generated file.
func (c *Config) validateStateless() []error {
	if !c.Stateless {
		return nil
	}

	var errs []error

	if c.Storage.Backend == "" || c.Storage.Backend == BackendLocal {
		errs = append(errs, errors.New("stateless: true needs storage.backend set to s3 or webdav, where Encore keeps its database between restarts"))
	}

	if c.Secrets.Key == "" && c.Secrets.KeyFile == "" {
		errs = append(errs, errors.New("stateless: true needs secrets.key (or ENCORE_SECRETS_KEY_FILE); a generated key would be lost on restart and stored secrets with it"))
	}

	return errs
}
