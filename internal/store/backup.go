package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BackupTo writes a consistent, compacted copy of the database to path with VACUUM INTO. path must not exist. It does not block writers for longer than the copy takes to read.
func (db *DB) BackupTo(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("backup target %s already exists", path)
	}

	if _, err := db.read.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("backing up database: %w", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restricting backup permissions: %w", err)
	}

	return nil
}

// Backup streams a consistent copy of the database to w. The copy is staged in a private temporary directory next to the database and removed afterwards.
func (db *DB) Backup(ctx context.Context, w io.Writer) error {
	dir, err := os.MkdirTemp(filepath.Dir(db.path), ".backup-")
	if err != nil {
		return fmt.Errorf("creating backup staging directory: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, FileName)
	if err := db.BackupTo(ctx, path); err != nil {
		return err
	}

	f, err := os.Open(path) //nolint:gosec // path is inside a directory we just created
	if err != nil {
		return fmt.Errorf("opening backup: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("writing backup: %w", err)
	}

	return nil
}
