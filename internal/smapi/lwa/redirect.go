package lwa

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ParseRedirectURL extracts the authorisation code and state from the URL the browser was redirected to, for when the redirect target is not reachable and the user pastes it instead. A bare query string is accepted too.
func ParseRedirectURL(pasted string) (code, state string, err error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", "", errors.New("lwa: pasted URL is empty")
	}

	query := pasted
	if i := strings.IndexByte(pasted, '?'); i >= 0 {
		u, err := url.Parse(pasted)
		if err != nil {
			return "", "", fmt.Errorf("lwa: parsing pasted URL: %w", err)
		}

		query = u.RawQuery
	}

	q, err := url.ParseQuery(query)
	if err != nil {
		return "", "", fmt.Errorf("lwa: parsing pasted URL: %w", err)
	}

	if e := q.Get("error"); e != "" {
		if d := q.Get("error_description"); d != "" {
			e += ": " + d
		}

		return "", "", fmt.Errorf("lwa: Amazon returned an error instead of a code: %s", e)
	}

	code, state = q.Get("code"), q.Get("state")
	if code == "" {
		return "", "", errors.New("lwa: pasted URL has no code parameter; copy the whole address from the browser")
	}

	if state == "" {
		return "", "", errors.New("lwa: pasted URL has no state parameter; copy the whole address from the browser")
	}

	return code, state, nil
}
