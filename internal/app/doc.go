// Package app assembles Encore's runtime: the database, secrets, a client per Kodi instance, and a catalog and title index per library group (Kodi instances that share one library database, or a single instance). It answers the Alexa adapter's questions about which Kodi a device controls and records handled requests.
package app
