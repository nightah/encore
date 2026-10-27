package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/secrets"
)

func (e *InputError) Error() string { return e.Field + ": " + e.Err.Error() }

func (e *InputError) Unwrap() error { return e.Err }

func (e *LockedError) Error() string {
	return fmt.Sprintf("too many failed sign-ins; try again in %s", e.RetryAfter.Round(time.Second))
}

// LocalAllowed reports whether password sign-in is available.
func (s Settings) LocalAllowed() bool {
	return s.Mode == ModeLocal || s.BreakGlass
}

// New prepares the service. When no admin account exists it generates a setup token and logs it.
func New(ctx context.Context, opts Options) (*Service, error) {
	if opts.DB == nil || opts.Secrets == nil || opts.Settings == nil {
		return nil, errors.New("auth: DB, Secrets and Settings are required")
	}

	s := &Service{
		db:         opts.DB,
		secrets:    opts.Secrets,
		settings:   opts.Settings,
		log:        opts.Logger,
		now:        opts.Now,
		httpClient: opts.HTTPClient,
		limiter:    newLimiter(),
		oidc:       newOIDCState(),
	}

	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}

	if s.now == nil {
		s.now = time.Now
	}

	if s.httpClient == nil {
		s.httpClient = http.DefaultClient
	}

	key, err := s.loadOrCreateKey(ctx, csrfKeyRef)
	if err != nil {
		return nil, err
	}

	s.csrfKey = key

	admin, err := s.admin(ctx)
	if err != nil {
		return nil, err
	}

	if admin == nil {
		s.setupToken = randomToken(18)
		s.log.Warn("no admin account exists yet; open the web interface and create one with this one-time setup token", "setup_token", s.setupToken)
	}

	return s, nil
}

// Settings returns the effective settings.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	return s.settings(ctx)
}

func (s *Service) loadOrCreateKey(ctx context.Context, ref string) ([]byte, error) {
	key, err := s.secrets.Get(ctx, ref)

	switch {
	case err == nil && len(key) == 32:
		return key, nil
	case err != nil && !errors.Is(err, secrets.ErrNotFound):
		return nil, fmt.Errorf("auth: reading %s: %w", ref, err)
	}

	key = make([]byte, 32)
	_, _ = rand.Read(key) // crypto/rand.Read never returns an error.

	if err := s.secrets.Put(ctx, ref, key); err != nil {
		return nil, fmt.Errorf("auth: storing %s: %w", ref, err)
	}

	return key, nil
}

// Authenticate identifies the admin behind r, or returns a nil Principal. Trusted-proxy headers are removed from r unless it came from a trusted proxy, so later handlers can never see spoofed ones. A trusted-header user outside the admin group yields ErrNotAdmin.
func (s *Service) Authenticate(w http.ResponseWriter, r *http.Request) (*Principal, error) {
	settings, err := s.settings(r.Context())
	if err != nil {
		return nil, err
	}

	trusted := s.fromTrustedProxy(r, settings)
	if !trusted {
		stripProxyHeaders(r, settings)
	}

	if settings.Mode == ModeHeader && trusted {
		if user := strings.TrimSpace(r.Header.Get(settings.Header.UserHeader)); user != "" {
			if g := settings.Header.AdminGroup; g != "" && !slices.Contains(splitGroups(r.Header.Get(settings.Header.GroupsHeader)), g) {
				return nil, ErrNotAdmin
			}

			return &Principal{Name: user, Method: ModeHeader}, nil
		}
	}

	return s.sessionPrincipal(w, r, settings)
}

// FromTrustedProxy reports whether r's TCP peer is in server.trusted_proxies.
func (s *Service) FromTrustedProxy(r *http.Request) bool {
	settings, err := s.settings(r.Context())
	if err != nil {
		return false
	}

	return s.fromTrustedProxy(r, settings)
}

func (s *Service) fromTrustedProxy(r *http.Request, settings Settings) bool {
	peer, ok := peerAddr(r)
	if !ok {
		return false
	}

	for _, p := range settings.TrustedProxies {
		if p.Contains(peer) {
			return true
		}
	}

	return false
}

func peerAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.Unmap(), true
}

func stripProxyHeaders(r *http.Request, settings Settings) {
	for _, h := range []string{settings.Header.UserHeader, settings.Header.GroupsHeader, "Remote-User", "Remote-Groups", "Remote-Name", "Remote-Email", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-For"} {
		if h != "" {
			r.Header.Del(h)
		}
	}
}

func splitGroups(s string) []string {
	var out []string

	for g := range strings.SplitSeq(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}

	return out
}

// ClientIP is the address used for rate limiting: the peer, or the right-most untrusted address in X-Forwarded-For when the peer is a trusted proxy.
func (s *Service) ClientIP(r *http.Request) string {
	peer, ok := peerAddr(r)
	if !ok {
		return r.RemoteAddr
	}

	settings, err := s.settings(r.Context())
	if err != nil || !s.fromTrustedProxy(r, settings) {
		return peer.String()
	}

	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}

		if !slices.ContainsFunc(settings.TrustedProxies, func(p netip.Prefix) bool { return p.Contains(addr.Unmap()) }) {
			return addr.Unmap().String()
		}
	}

	return peer.String()
}

// Secure reports whether r reached Encore over HTTPS, directly or through a trusted proxy that says so.
func (s *Service) Secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") && s.FromTrustedProxy(r)
}

// ExternalBase is the URL the browser uses to reach Encore's base path, e.g. https://encore.example.net/encore, without a trailing slash.
func (s *Service) ExternalBase(r *http.Request) string {
	scheme := "http"
	if s.Secure(r) {
		scheme = "https"
	}

	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.FromTrustedProxy(r) {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}

	return scheme + "://" + host + strings.TrimSuffix(basepath.From(r.Context()), "/")
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.

	return base64.RawURLEncoding.EncodeToString(b)
}
