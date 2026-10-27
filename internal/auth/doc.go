// Package auth signs admins in to Encore's web interface.
//
// Three modes are supported. Local mode keeps one admin account (an argon2id password hash and an optional TOTP secret); the first admin is created with a one-time setup token printed to the log. OIDC mode signs in through an OpenID Connect provider with the authorisation code flow, PKCE, state and nonce, and can require membership of an admin group. Header mode trusts a reverse proxy's Remote-User and Remote-Groups headers, but only on connections from server.trusted_proxies; the headers are stripped from every other request.
//
// Local and OIDC sign-ins create a session: a random token in an HttpOnly cookie, stored in the database only as a SHA-256 hash, with a sliding expiry. State-changing requests must also carry a CSRF token derived from a per-browser cookie.
package auth
