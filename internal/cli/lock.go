package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// lockDataDir doesn't wait, and creates the directory if needed. It returns an error wrapping errLocked when another Encore holds the lock.
func lockDataDir(dir string) (*dirLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	path := filepath.Join(dir, lockFileName)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // inside the operator's data directory
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	if err := lockFile(f); err != nil {
		_ = f.Close()

		if !errors.Is(err, errLocked) {
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}

		holder := ""
		if b, err := os.ReadFile(path); err == nil { //nolint:gosec // inside the operator's data directory
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				holder = fmt.Sprintf(" (pid %d)", pid)
			}
		}

		return nil, fmt.Errorf("another Encore%s is using the data directory %s: %w", holder, dir, errLocked)
	}

	// Record the holder for the message above; the lock itself is what counts.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}

	return &dirLock{f: f}, nil
}

func (l *dirLock) Close() error {
	return l.f.Close()
}
