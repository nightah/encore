package statestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nightah/encore/internal/blobstore"
)

// Restore downloads the saved database to path, reporting whether one existed. A fresh deployment has none, and Encore starts with an empty database.
func Restore(ctx context.Context, s blobstore.Store, path string) (bool, error) {
	r, err := s.Get(ctx, KeyDatabase)

	switch {
	case errors.Is(err, blobstore.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("fetching saved database: %w", err)
	}

	defer r.Close()

	zr, err := gzip.NewReader(r)
	if err != nil {
		return false, fmt.Errorf("reading saved database: %w", err)
	}

	defer zr.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-*")
	if err != nil {
		return false, err
	}

	defer os.Remove(tmp.Name())

	_, err = io.Copy(tmp, zr) //nolint:gosec // the archive is Encore's own database, written by Save
	if err = errors.Join(err, tmp.Sync(), tmp.Close()); err != nil {
		return false, fmt.Errorf("restoring saved database: %w", err)
	}

	return true, os.Rename(tmp.Name(), path)
}

// Run saves on every interval when the database changed, and once more when ctx ends so a clean shutdown loses nothing.
func (s *Saver) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	s.checkOwner(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			if _, err := s.Save(final); err != nil {
				s.Log.Error("saving database on shutdown", "error", err)
			}

			s.release(final)

			cancel()

			return
		case <-t.C:
			if _, err := s.Save(ctx); err != nil {
				s.Log.Warn("saving database to storage", "error", err)
			}

			s.heartbeat(ctx)
		}
	}
}

// Save uploads a consistent copy of the database if it differs from the last one saved, reporting whether it uploaded. The previous copy is kept as a fallback.
func (s *Saver) Save(ctx context.Context) (bool, error) {
	var raw bytes.Buffer
	if err := s.DB.Backup(ctx, &raw); err != nil {
		return false, fmt.Errorf("copying database: %w", err)
	}

	sum := sha256.Sum256(raw.Bytes())
	if sum == s.last {
		return false, nil
	}

	gz, err := compress(raw.Bytes())
	if err != nil {
		return false, err
	}

	if err := keepPrevious(ctx, s.Store); err != nil {
		s.Log.Warn("keeping previous database copy", "error", err)
	}

	if err := s.Store.Put(ctx, KeyDatabase, bytes.NewReader(gz.Bytes()), int64(gz.Len())); err != nil {
		return false, fmt.Errorf("uploading database: %w", err)
	}

	s.last = sum
	s.Log.Debug("saved database to storage", "bytes", gz.Len())

	return true, nil
}

// Upload replaces the saved database with the database file at path, keeping the current copy as the previous one as Save does. It is for restoring a backup while no Encore is running against s; see ActiveOwner.
func Upload(ctx context.Context, s blobstore.Store, path string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // the caller's verified database file
	if err != nil {
		return fmt.Errorf("reading database: %w", err)
	}

	gz, err := compress(raw)
	if err != nil {
		return err
	}

	if err := keepPrevious(ctx, s); err != nil {
		return fmt.Errorf("keeping the current database as %s: %w", KeyPrevious, err)
	}

	if err := s.Put(ctx, KeyDatabase, bytes.NewReader(gz.Bytes()), int64(gz.Len())); err != nil {
		return fmt.Errorf("uploading database: %w", err)
	}

	return nil
}

func compress(raw []byte) (*bytes.Buffer, error) {
	var gz bytes.Buffer

	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return &gz, nil
}

func keepPrevious(ctx context.Context, s blobstore.Store) error {
	r, err := s.Get(ctx, KeyDatabase)
	if errors.Is(err, blobstore.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	return s.Put(ctx, KeyPrevious, bytes.NewReader(data), int64(len(data)))
}

// ActiveOwner reads the owner marker and reports whether its heartbeat is recent enough at now that the Encore which wrote it may still be running against s. It returns the marker's owner and heartbeat; active is false when there is no marker or it is stale.
func ActiveOwner(ctx context.Context, s blobstore.Store, now time.Time) (name string, heartbeat time.Time, active bool, err error) {
	var o owner

	err = readJSON(ctx, s, KeyOwner, &o)

	switch {
	case errors.Is(err, blobstore.ErrNotFound):
		return "", time.Time{}, false, nil
	case err != nil:
		return "", time.Time{}, false, fmt.Errorf("reading owner marker: %w", err)
	}

	return o.Owner, o.Heartbeat, now.Sub(o.Heartbeat) < ownerStale, nil
}

func (s *Saver) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// checkOwner warns when another Encore appears to be using the same storage, then claims it.
func (s *Saver) checkOwner(ctx context.Context) {
	var other owner

	err := readJSON(ctx, s.Store, KeyOwner, &other)
	if err == nil && other.Owner != s.Owner && s.now().Sub(other.Heartbeat) < ownerStale {
		s.Log.Warn("another Encore seems to be using this storage; running two at once makes them overwrite each other's database", "other", other.Owner, "last_seen", other.Heartbeat)
	}

	s.heartbeat(ctx)
}

// release removes the owner marker on shutdown if it is still this process's, so the storage is free straight away for another Encore or a restore.
func (s *Saver) release(ctx context.Context) {
	var o owner
	if err := readJSON(ctx, s.Store, KeyOwner, &o); err != nil || o.Owner != s.Owner {
		return
	}

	if err := s.Store.Delete(ctx, KeyOwner); err != nil {
		s.Log.Debug("removing owner marker", "error", err)
	}
}

func (s *Saver) heartbeat(ctx context.Context) {
	b, err := json.Marshal(owner{Owner: s.Owner, Heartbeat: s.now().UTC()})
	if err != nil {
		return
	}

	if err := s.Store.Put(ctx, KeyOwner, bytes.NewReader(b), int64(len(b))); err != nil {
		s.Log.Debug("writing owner marker", "error", err)
	}
}

func readJSON(ctx context.Context, s blobstore.Store, key string, v any) error {
	r, err := s.Get(ctx, key)
	if err != nil {
		return err
	}

	defer r.Close()

	return json.NewDecoder(r).Decode(v)
}
