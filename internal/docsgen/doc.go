// Package docsgen renders the reference pages of the documentation site (docs/) from the code, so they cannot drift from it: the configuration reference from package config's structs, defaults and doc comments, the CLI reference from the cobra command tree, the admin API's endpoints page from the OpenAPI document (internal/api/openapi.json, itself generated from package api), and the voice command reference from the interaction model sources in i18n/.
//
// The pages aren't committed: `go generate ./internal/docsgen` writes them, and the docs site's dev and build scripts run it first, so they always match the code and the translations Crowdin syncs into i18n/.
package docsgen
