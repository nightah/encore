package oidctest

import (
	"crypto/rsa"
	"net/http/httptest"
	"sync"
)

// Options configure the provider.
type Options struct {
	ClientID     string
	ClientSecret string
	Subject      string
	Username     string
	Groups       []string
	// GroupsInUserInfoOnly leaves groups out of the ID token, as some providers do.
	GroupsInUserInfoOnly bool
	// ClaimsInUserInfoOnly leaves the username and groups out of the ID token, as some providers do by default.
	ClaimsInUserInfoOnly bool
	// UserInfoSubject, when set, makes the userinfo endpoint answer for another subject than the ID token's.
	UserInfoSubject string
	// UserInfoUsername, when set, makes the userinfo endpoint report another username than the ID token's.
	UserInfoUsername string
	// WrongNonce makes the ID token carry a different nonce.
	WrongNonce bool
	// AuthMethod limits how the token endpoint accepts the client secret: client_secret_basic or client_secret_post. Empty accepts either.
	AuthMethod string
}

// Server is the fake provider. Its URL is the issuer.
type Server struct {
	*httptest.Server

	opts Options
	key  *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]grant
	auths []string
}

type grant struct {
	nonce     string
	challenge string
	redirect  string
}
