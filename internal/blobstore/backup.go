package blobstore

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Backup writes a gzipped, timestamped copy of db under prefix, then deletes all but the newest keep backups. It returns the new backup's key. Scheduling is the caller's concern.
func Backup(ctx context.Context, db Database, s Store, prefix string, keep int) (string, error) {
	if keep < 1 {
		return "", fmt.Errorf("keep %d must be at least 1", keep)
	}

	if err := validatePrefix(prefix); err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp(filepath.Dir(db.Path()), ".backup-")
	if err != nil {
		return "", fmt.Errorf("creating backup staging directory: %w", err)
	}
	defer os.RemoveAll(dir)

	raw := filepath.Join(dir, "encore.db")
	if err := db.BackupTo(ctx, raw); err != nil {
		return "", err
	}

	gzPath := raw + ".gz"

	size, err := gzipFile(raw, gzPath)
	if err != nil {
		return "", err
	}

	f, err := os.Open(gzPath) //nolint:gosec // inside a directory we just created
	if err != nil {
		return "", fmt.Errorf("opening backup: %w", err)
	}
	defer f.Close()

	key := joinPrefix(strings.TrimSuffix(prefix, "/"), backupNamePrefix+time.Now().UTC().Format(backupTimeLayout)+backupNameSuffix)

	if err := s.Put(ctx, key, f, size); err != nil {
		return "", fmt.Errorf("uploading backup: %w", err)
	}

	backups, err := ListBackups(ctx, s, prefix)
	if err != nil {
		return key, fmt.Errorf("listing backups to prune: %w", err)
	}

	var errs []error

	for _, old := range backups[min(keep, len(backups)):] {
		if err := s.Delete(ctx, old.Key); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return key, fmt.Errorf("pruning old backups: %w", err)
	}

	return key, nil
}

// ListBackups returns the backups under prefix, newest first.
func ListBackups(ctx context.Context, s Store, prefix string) ([]Object, error) {
	objs, err := s.List(ctx, joinPrefix(strings.TrimSuffix(prefix, "/"), backupNamePrefix))
	if err != nil {
		return nil, err
	}

	objs = slices.DeleteFunc(objs, func(o Object) bool {
		_, ok := BackupTime(o.Key)

		return !ok
	})

	slices.Reverse(objs)

	return objs, nil
}

// BackupTime returns when the backup named by key was taken, from its name; ok is false if key isn't a backup's.
func BackupTime(key string) (t time.Time, ok bool) {
	name := key[strings.LastIndexByte(key, '/')+1:]
	if !strings.HasPrefix(name, backupNamePrefix) || !strings.HasSuffix(name, backupNameSuffix) {
		return time.Time{}, false
	}

	t, err := time.Parse(backupTimeLayout, strings.TrimSuffix(strings.TrimPrefix(name, backupNamePrefix), backupNameSuffix))

	return t, err == nil
}

// FetchBackup downloads the backup at key and decompresses it into a new private file in dir, returning the file's path. dir should be on the same filesystem as the database so the file can be renamed into place. The caller removes the file.
func FetchBackup(ctx context.Context, s Store, key, dir string) (string, error) {
	rc, err := s.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("fetching backup %s: %w", key, err)
	}
	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		return "", fmt.Errorf("decompressing backup %s: %w", key, err)
	}
	defer gz.Close()

	f, err := os.CreateTemp(dir, ".restore-*.db")
	if err != nil {
		return "", fmt.Errorf("creating restore file: %w", err)
	}

	_, err = io.Copy(f, ctxReader{ctx: ctx, r: gz})
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		_ = os.Remove(f.Name())

		return "", fmt.Errorf("decompressing backup %s: %w", key, err)
	}

	return f.Name(), nil
}

func gzipFile(src, dst string) (int64, error) {
	in, err := os.Open(src) //nolint:gosec // inside a directory Backup created
	if err != nil {
		return 0, fmt.Errorf("opening backup: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // inside a directory Backup created
	if err != nil {
		return 0, fmt.Errorf("creating compressed backup: %w", err)
	}

	gz := gzip.NewWriter(out)

	_, err = io.Copy(gz, in)
	err = errors.Join(err, gz.Close(), out.Close())
	if err != nil {
		return 0, fmt.Errorf("compressing backup: %w", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		return 0, fmt.Errorf("compressing backup: %w", err)
	}

	return info.Size(), nil
}
