//go:build unix

package kodi

import (
	"cmp"
	"syscall"

	"golang.org/x/sys/unix"
)

// reuseMDNSPort lets the mDNS socket share port 5353 with other mDNS software on the host, such as Avahi; every socket in the group receives each multicast answer.
func reuseMDNSPort(_, _ string, c syscall.RawConn) error {
	var err error

	ctrlErr := c.Control(func(fd uintptr) {
		if err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err == nil {
			err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	})

	return cmp.Or(ctrlErr, err)
}
