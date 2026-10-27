// Package api is Encore's admin HTTP API under /api/v1/, consumed by the web interface. The admin API docs in docs/src/content/docs/reference/api/ describe the contract; the OpenAPI 3.1 document generated from this package is served at /api/v1/openapi.json and committed as openapi.json for the web client generator.
//
// Every route requires an admin (a session cookie, or a trusted proxy's headers in header mode) unless it is marked public. State-changing requests must carry the X-CSRF-Token from GET /api/v1/auth/state and pass http.CrossOriginProtection. Errors are RFC 9457 problem documents.
package api
