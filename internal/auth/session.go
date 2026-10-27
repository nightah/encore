package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/store"
)

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

// StartSession signs username in on this browser.
func (s *Service) StartSession(ctx context.Context, w http.ResponseWriter, r *http.Request, username string) error {
	settings, err := s.settings(ctx)
	if err != nil {
		return err
	}

	token := randomToken(32)

	sess := store.Session{IDHash: hashToken(token), Username: username, ExpiresAt: s.now().Add(settings.SessionTTL)}
	if err := s.db.Sessions().Create(ctx, &sess); err != nil {
		return err
	}

	// A previous session on this browser is replaced, never reused, so a planted cookie cannot be upgraded by signing in.
	if old, err := r.Cookie(SessionCookie); err == nil {
		_ = s.db.Sessions().Delete(ctx, hashToken(old.Value))
	}

	s.setCookie(w, r, SessionCookie, token, settings.SessionTTL)

	return nil
}

func (s *Service) sessionPrincipal(w http.ResponseWriter, r *http.Request, settings Settings) (*Principal, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil //nolint:nilnil // no session is not an error
	}

	ctx := r.Context()
	hash := hashToken(c.Value)

	sess, err := s.db.Sessions().Get(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		s.clearCookie(w, r, SessionCookie)

		return nil, nil //nolint:nilnil // an expired session is not an error
	}

	if err != nil {
		return nil, err
	}

	if now := s.now(); now.Sub(sess.LastSeenAt) >= touchEvery {
		if err := s.db.Sessions().Touch(ctx, hash, now.Add(settings.SessionTTL)); err != nil {
			return nil, fmt.Errorf("extending session: %w", err)
		}

		s.setCookie(w, r, SessionCookie, c.Value, settings.SessionTTL)
	}

	// A session in header mode was started with the break-glass password, so it must not pass for a trusted-header request.
	method := settings.Mode
	if method == ModeHeader {
		method = ModeLocal
	}

	return &Principal{Name: sess.Username, Method: method, SessionCreated: sess.CreatedAt}, nil
}

// Logout ends the session on this browser.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := s.db.Sessions().Delete(r.Context(), hashToken(c.Value)); err != nil {
			return err
		}
	}

	s.clearCookie(w, r, SessionCookie)

	return nil
}

// EndOtherSessions signs out every session except the one r carries (all of them when r is nil), e.g. after the sign-in mode or the admin password changed.
func (s *Service) EndOtherSessions(ctx context.Context, r *http.Request) error {
	var keep []byte

	if r != nil {
		if c, err := r.Cookie(SessionCookie); err == nil {
			keep = hashToken(c.Value)
		}
	}

	_, err := s.db.Sessions().DeleteAllExcept(ctx, keep)

	return err
}

func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, name, value string, ttl time.Duration) {
	// Secure follows the request's scheme so plain-HTTP installs on a LAN still work; behind an HTTPS proxy it is always set.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // HttpOnly and SameSite are always set; Secure is set whenever the request came over HTTPS
		Name:     name,
		Value:    value,
		Path:     basepath.From(r.Context()),
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // as setCookie
		Name:     name,
		Path:     basepath.From(r.Context()),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}
