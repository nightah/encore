package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/pressly/goose/v3"
)

// InspectFile checks the database file at path without migrating or changing it: it must be SQLite and pass PRAGMA integrity_check. It returns the file's schema version and secret rows. The file should not be in use by anything else.
func InspectFile(ctx context.Context, path string) (FileInfo, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "query_only(1)")

	u := url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return FileInfo{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	rows, err := queryAll(ctx, db, func(s scanner) (string, error) {
		var line string
		err := s.Scan(&line)

		return line, err
	}, `PRAGMA integrity_check`)
	if err != nil {
		return FileInfo{}, fmt.Errorf("%s is not a readable SQLite database: %w", path, err)
	}

	if len(rows) != 1 || rows[0] != "ok" {
		return FileInfo{}, fmt.Errorf("%s failed its integrity check: %s", path, strings.Join(rows, "; "))
	}

	var info FileInfo

	if has, err := hasTable(ctx, db, goose.DefaultTablename); err != nil {
		return FileInfo{}, err
	} else if has {
		var v sql.NullInt64
		if err := db.QueryRowContext(ctx, `SELECT MAX(version_id) FROM `+goose.DefaultTablename).Scan(&v); err != nil {
			return FileInfo{}, fmt.Errorf("reading schema version: %w", err)
		}

		info.SchemaVersion = v.Int64
	}

	if has, err := hasTable(ctx, db, "secrets"); err != nil {
		return FileInfo{}, err
	} else if has {
		if info.Secrets, err = queryAll(ctx, db, scanSecret, `SELECT `+secretColumns+` FROM secrets ORDER BY id`); err != nil {
			return FileInfo{}, fmt.Errorf("reading secrets: %w", err)
		}
	}

	return info, nil
}

func hasTable(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("reading schema: %w", err)
	}

	return n > 0, nil
}

// SchemaVersion returns the newest migration this build carries. Open migrates an older database up to it; a database with a newer version was written by a newer Encore.
func SchemaVersion() (int64, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return 0, err
	}

	var latest int64

	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", name, err)
		}

		latest = max(latest, v)
	}

	if latest == 0 {
		return 0, errors.New("no migrations embedded")
	}

	return latest, nil
}
