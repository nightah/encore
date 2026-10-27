// Command fakekodi serves the in-memory fake Kodi on a fixed address, for running the web interface's end-to-end tests (and trying Encore) against the real server without a Kodi.
//
//	go run ./internal/backend/kodi/fakekodi/cmd/fakekodi -addr 127.0.0.1:18080
//
// Add http://127.0.0.1:18080/jsonrpc as a Kodi instance; the WebSocket is on the same path.
package main
