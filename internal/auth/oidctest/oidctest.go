package oidctest

import (
	"cmp"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// New starts a provider and stops it when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()

	if opts.ClientID == "" {
		opts.ClientID = DefaultClientID
	}

	if opts.ClientSecret == "" {
		opts.ClientSecret = DefaultClientSecret
	}

	if opts.Subject == "" {
		opts.Subject = "user-1"
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{opts: opts, key: key, codes: map[string]grant{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /keys", s.keys)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("GET /userinfo", s.userinfo)

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)

	return s
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                s.URL,
		"authorization_endpoint":                s.URL + "/authorize",
		"token_endpoint":                        s.URL + "/token",
		"userinfo_endpoint":                     s.URL + "/userinfo",
		"jwks_uri":                              s.URL + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (s *Server) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &s.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

// authorize approves at once and redirects back with a code, as a provider does after the user consents.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.opts.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "bad authorisation request", http.StatusBadRequest)

		return
	}

	code := rand.Text()

	s.mu.Lock()
	s.codes[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	s.mu.Unlock()

	u, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)

		return
	}

	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound) //nolint:gosec // a provider redirects to the client's redirect_uri; this one only runs in tests
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}

	method := "client_secret_basic"

	id, secret, ok := r.BasicAuth()
	if !ok {
		method = "client_secret_post"
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}

	s.mu.Lock()
	s.auths = append(s.auths, method)
	s.mu.Unlock()

	if id != s.opts.ClientID || secret != s.opts.ClientSecret || (s.opts.AuthMethod != "" && method != s.opts.AuthMethod) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid_client"})

		return
	}

	s.mu.Lock()
	g, ok := s.codes[r.PostForm.Get("code")]
	delete(s.codes, r.PostForm.Get("code"))
	s.mu.Unlock()

	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge || r.PostForm.Get("redirect_uri") != g.redirect {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})

		return
	}

	nonce := g.nonce
	if s.opts.WrongNonce {
		nonce = "not-the-nonce"
	}

	claims := map[string]any{
		"iss": s.URL, "aud": cmp.Or(s.opts.Audience, s.opts.ClientID), "sub": s.opts.Subject, "nonce": nonce,
		"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
	}

	if !s.opts.ClaimsInUserInfoOnly {
		claims["preferred_username"] = s.opts.Username

		if !s.opts.GroupsInUserInfoOnly {
			claims["groups"] = s.opts.Groups
		}
	}

	idToken, err := s.sign(claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	resp := map[string]any{"access_token": "access-" + s.opts.Subject, "token_type": "Bearer", "expires_in": 300, "id_token": idToken}
	if s.opts.NoIDToken {
		delete(resp, "id_token")
	}

	writeJSON(w, resp)
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer access-"+s.opts.Subject {
		http.Error(w, "unauthorised", http.StatusUnauthorized)

		return
	}

	sub := s.opts.Subject
	if s.opts.UserInfoSubject != "" {
		sub = s.opts.UserInfoSubject
	}

	username := s.opts.Username
	if s.opts.UserInfoUsername != "" {
		username = s.opts.UserInfoUsername
	}

	writeJSON(w, map[string]any{"sub": sub, "preferred_username": username, "groups": s.opts.Groups})
}

func (s *Server) sign(claims map[string]any) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		return "", err
	}

	return jwt.Signed(signer).Claims(claims).Serialize()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TokenAuths lists how each token request sent the client secret, in order: client_secret_basic or client_secret_post.
func (s *Server) TokenAuths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.auths)
}
