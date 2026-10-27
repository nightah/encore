package blobstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

var _ Store = (*Local)(nil)

// NewLocal returns a store rooted at dir, creating it with mode 0700.
func NewLocal(dir string) (*Local, error) {
	if dir == "" {
		return nil, errors.New("local storage directory must not be empty")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating storage directory: %w", err)
	}

	return &Local{dir: dir}, nil
}

// Dir returns the root directory.
func (l *Local) Dir() string { return l.dir }

func (l *Local) root() (*os.Root, error) {
	r, err := os.OpenRoot(l.dir)
	if err != nil {
		return nil, fmt.Errorf("opening storage directory: %w", err)
	}

	return r, nil
}

// Put implements Store.
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	root, err := l.root()
	if err != nil {
		return err
	}
	defer root.Close()

	dir := path.Dir(key)
	if err := root.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating directory for %s: %w", key, err)
	}

	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	tmp := path.Join(dir, tmpPrefix+hex.EncodeToString(rnd[:]))

	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", key, err)
	}

	n, err := io.Copy(f, ctxReader{ctx, r})
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("wrote %d bytes, expected %d", n, size)
	}

	if err == nil {
		err = f.Sync()
	}

	err = errors.Join(err, f.Close())

	if err == nil {
		err = root.Rename(tmp, key)
	}

	if err != nil {
		_ = root.Remove(tmp)

		return fmt.Errorf("writing %s: %w", key, err)
	}

	return nil
}

// Get implements Store.
func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	root, err := l.root()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f, err := root.Open(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}

		return nil, fmt.Errorf("opening %s: %w", key, err)
	}

	if info, err := f.Stat(); err != nil || info.IsDir() {
		_ = f.Close()

		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}

	return f, nil
}

// Delete implements Store.
func (l *Local) Delete(_ context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	root, err := l.root()
	if err != nil {
		return err
	}
	defer root.Close()

	if info, err := root.Lstat(key); err == nil && info.IsDir() {
		return nil
	}

	if err := root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("deleting %s: %w", key, err)
	}

	return nil
}

// List implements Store.
func (l *Local) List(ctx context.Context, prefix string) ([]Object, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}

	root, err := l.root()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	start := "."
	if i := strings.LastIndexByte(prefix, '/'); i >= 0 {
		start = prefix[:i]
	}

	var out []Object

	err = fs.WalkDir(root.FS(), start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}

			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if d.IsDir() {
			// Skip subtrees that cannot contain a match.
			if p != "." && !strings.HasPrefix(p+"/", prefix) && !strings.HasPrefix(prefix, p+"/") {
				return fs.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), tmpPrefix) || !strings.HasPrefix(p, prefix) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}

			return err
		}

		out = append(out, Object{Key: p, Size: info.Size(), Modified: info.ModTime().UTC()})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %q: %w", prefix, err)
	}

	slices.SortFunc(out, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })

	return out, nil
}

// Probe implements Store.
func (l *Local) Probe(ctx context.Context) error {
	return probe(ctx, l)
}
