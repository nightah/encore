//go:build !unix

package kodi

import "syscall"

// reuseMDNSPort leaves the socket as it is; when something else holds port 5353, browsing falls back to a random port.
func reuseMDNSPort(_, _ string, _ syscall.RawConn) error { return nil }
