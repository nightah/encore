package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
)

// CSRFToken returns the CSRF token for this browser, setting the browser's CSRF cookie first if it has none. The token is an HMAC of the cookie under a server key, so it can only be obtained by reading a same-origin response and cannot be forged from the cookie alone.
func (s *Service) CSRFToken(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(csrfCookie)
	if err != nil || len(c.Value) < 32 {
		value := randomToken(32)
		s.setCookie(w, r, csrfCookie, value, csrfCookieTTL)

		return s.csrfFor(value)
	}

	return s.csrfFor(c.Value)
}

// CheckCSRF reports whether r carries the CSRF token matching its cookie.
func (s *Service) CheckCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	if err != nil || len(c.Value) < 32 {
		return false
	}

	got := r.Header.Get(CSRFHeader)

	return got != "" && hmac.Equal([]byte(got), []byte(s.csrfFor(c.Value)))
}

func (s *Service) csrfFor(cookie string) string {
	m := hmac.New(sha256.New, s.csrfKey)
	m.Write([]byte("Encore csrf\x00" + cookie))

	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
