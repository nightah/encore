// Package statestore keeps Encore's database in the storage backend for stateless deployments: containers without a persistent volume restore the latest copy at startup and save a new one whenever it changes, so configuration, Amazon tokens and device links survive restarts.
//
// It assumes one running Encore per storage location. An owner marker with a heartbeat makes a second copy visible in the logs, but object stores offer no reliable lock, so it warns rather than refuses. A clean shutdown removes the marker; encore restore refuses to replace the saved database while it is fresh.
package statestore
