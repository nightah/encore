package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// DefaultPath returns the database path within dataDir.
func DefaultPath(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Open opens or creates the database at opts.Path and applies pending migrations.
func Open(ctx context.Context, opts Options) (*DB, error) {
	if opts.Path == "" {
		return nil, errors.New("store: database path must not be empty")
	}

	path, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("resolving database path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	// Create the file up front so it gets 0600; SQLite gives the -wal and -shm files the same mode.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // the operator chooses the database path
	if err != nil {
		return nil, fmt.Errorf("creating database file: %w", err)
	}

	_ = f.Close()

	db := &DB{
		path:      path,
		log:       opts.Logger,
		now:       opts.Now,
		retention: opts.RequestLog.withDefaults(),
	}

	if db.log == nil {
		db.log = slog.New(slog.DiscardHandler)
	}

	if db.now == nil {
		db.now = time.Now
	}

	if db.write, err = sql.Open("sqlite", dsn(path, false)); err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	db.write.SetMaxOpenConns(1)

	// The writer must exist and have switched the file to WAL before read-only connections attach.
	if err := db.write.PingContext(ctx); err != nil {
		_ = db.write.Close()

		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}

	if err := migrate(ctx, db.write); err != nil {
		_ = db.write.Close()

		return nil, err
	}

	if db.read, err = sql.Open("sqlite", dsn(path, true)); err != nil {
		_ = db.write.Close()

		return nil, fmt.Errorf("opening read pool: %w", err)
	}

	readConns := opts.ReadConns
	if readConns <= 0 {
		readConns = max(4, runtime.GOMAXPROCS(0))
	}

	db.read.SetMaxOpenConns(readConns)
	db.read.SetMaxIdleConns(readConns)

	return db, nil
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")

	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		// BEGIN IMMEDIATE takes the write lock at the start of a transaction, so a read-then-write transaction cannot fail with SQLITE_BUSY part way through.
		q.Set("_txlock", "immediate")
	}

	u := url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}

	return u.String()
}

// Path returns the database file path.
func (db *DB) Path() string { return db.path }

// Close closes both connection pools.
func (db *DB) Close() error {
	return errors.Join(db.read.Close(), db.write.Close())
}

// Ping checks both pools.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.write.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging writer: %w", err)
	}

	if err := db.read.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging read pool: %w", err)
	}

	return nil
}

func (db *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()

		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing: %w", mapErr(err))
	}

	return nil
}

// stamp returns the current time at the millisecond precision stored in the database, so structs returned from writes compare equal to those read back.
func (db *DB) stamp() time.Time { return db.now().UTC().Truncate(time.Millisecond) }

// mapErr classifies driver errors into ErrNotFound, ErrConflict and ErrInvalid.
func mapErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}

	var se *sqlite.Error
	if !errors.As(err, &se) {
		return err
	}

	switch se.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return fmt.Errorf("%w: %w", ErrConflict, err)
	case sqlite3.SQLITE_CONSTRAINT_CHECK, sqlite3.SQLITE_CONSTRAINT_NOTNULL, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	default:
		return err
	}
}

// affected returns ErrNotFound when an update or delete matched nothing.
func affected(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

func toMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UnixMilli()
}

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}

	return time.UnixMilli(ms).UTC()
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}
