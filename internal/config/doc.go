// Package config loads Encore's configuration.
//
// Sources are layered, later ones winning: built-in defaults, an optional YAML file, ENCORE_* environment variables, then command-line flags that were explicitly set. An environment variable is the key upper-cased with dots as underscores, so server.address is ENCORE_SERVER_ADDRESS and server.trusted_proxies is ENCORE_SERVER_TRUSTED_PROXIES. Any setting's variable with _FILE appended is read from the named file instead, for container secrets.
package config
