// Package blobstore is pluggable object storage for library snapshots, config exports and database backups. Backends are the local filesystem, S3-compatible services and WebDAV.
//
// Keys are slash-separated relative paths such as "backups/encore-20261026T030000.000Z.db.gz". Each backend maps them under its configured root or prefix.
package blobstore
