//go:build unix

package cli

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive flock on f without blocking.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errLocked
	}

	return err
}
