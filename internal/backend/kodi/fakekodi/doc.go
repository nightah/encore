// Package fakekodi is an in-memory Kodi JSON-RPC server for tests.
//
// It speaks JSON-RPC 2.0 over HTTP POST and WebSocket on the same /jsonrpc path, serves a small library, records every call, and pushes notifications to WebSocket clients. Behaviour is deliberately shallow: enough for Encore's client, matcher and handlers to be exercised without a real Kodi. Contract tests against headless Kodi keep it honest.
package fakekodi
