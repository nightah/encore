// Package lwa authorises Encore against the Skill Management API with Login with Amazon (LWA).
//
// The user creates their own LWA security profile and gives Encore its client ID and secret; Encore never ships a client ID of its own. Three ways to obtain a refresh token are supported:
//
//  1. Authorisation code grant with PKCE and state, redirected to Encore's public HTTPS callback.
//  2. The same grant redirected to an address nothing listens on (e.g. http://127.0.0.1:9090/cb); the user pastes the URL the browser ended up at and ParseRedirectURL extracts the code.
//  3. A refresh token pasted from `ask util generate-lwa-tokens`, checked with FromRefreshToken.
//
// Access tokens last an hour; the refresh token lasts until revoked. TokenSource refreshes and persists through a TokenStore, and reports a revoked or expired grant as ErrReauthorise.
package lwa
