// Package store is Encore's SQLite database: schema migrations, repositories and backups.
//
// The database uses the pure-Go modernc.org/sqlite driver in WAL mode. Writes go through a single connection so they never contend for the write lock; reads use a separate read-only pool that runs concurrently with the writer. Timestamps are stored as Unix milliseconds in UTC. A zero int64 foreign key means "none" and is stored as NULL.
package store
