// Package library keeps an in-memory catalog of one backend instance's media library, so the voice layer can match titles without asking the media centre every time.
//
// A Catalog is loaded in full by Sync and kept current by deltas of recently added items, by backend events (an updated item is fetched again, a removed one dropped, a finished scan triggers a delta, a reconnect a full sync) and by a periodic safety-net delta. Lookup asks the backend directly when a title isn't in the catalog yet, which covers items Kodi dates by file time and so never shows as recently added.
package library
