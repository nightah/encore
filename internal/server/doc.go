// Package server runs Encore's single HTTP listener.
//
// Routes, served both under the configured base path and at /:
//
//	POST /api/v1/alexa    Alexa custom skill endpoint (the only route that must be public)
//	GET  /api/v1/healthz  liveness and version
//	     /api/v1/...      the admin API (internal/api), when configured
//	GET  /api/            Swagger UI for the admin API's OpenAPI document
//	     /api/...         JSON 404 for anything unknown
//	GET  /...             the embedded web UI, with index.html as the fallback
package server
