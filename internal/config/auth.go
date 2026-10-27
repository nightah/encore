package config

import (
	"errors"
	"fmt"
	"regexp"
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func defaultAuth() Auth {
	return Auth{
		SessionHours:    168,
		LockoutAttempts: 5,
		LockoutMinutes:  15,
		OIDC:            AuthOIDC{Scopes: []string{"openid", "profile", "email", "groups"}, GroupsClaim: "groups"},
		Header:          AuthHeader{UserHeader: "Remote-User", GroupsHeader: "Remote-Groups"},
	}
}

func (a *Auth) validate(trustedProxies []string) []error {
	var errs []error

	switch a.Mode {
	case "", AuthModeLocal:
	case AuthModeOIDC:
		if a.OIDC.Issuer == "" || a.OIDC.ClientID == "" || a.OIDC.ClientSecret == "" {
			errs = append(errs, errors.New("auth.mode oidc needs auth.oidc.issuer, auth.oidc.client_id and auth.oidc.client_secret"))
		}
	case AuthModeHeader:
		if len(trustedProxies) == 0 {
			errs = append(errs, errors.New("auth.mode header needs server.trusted_proxies, or anyone could send the header"))
		}
	default:
		errs = append(errs, fmt.Errorf("auth.mode %q must be local, oidc or header", a.Mode))
	}

	if a.OIDC.Issuer != "" {
		if err := validateHTTPURL(a.OIDC.Issuer); err != nil {
			errs = append(errs, fmt.Errorf("auth.oidc.issuer: %w", err))
		}
	}

	if a.OIDC.RedirectURI != "" {
		if err := validateHTTPURL(a.OIDC.RedirectURI); err != nil {
			errs = append(errs, fmt.Errorf("auth.oidc.redirect_uri: %w", err))
		}
	}

	if a.SessionHours < 1 {
		errs = append(errs, fmt.Errorf("auth.session_hours %d must be at least 1", a.SessionHours))
	}

	if a.LockoutAttempts < 1 || a.LockoutMinutes < 1 {
		errs = append(errs, errors.New("auth.lockout_attempts and auth.lockout_minutes must be at least 1"))
	}

	if !headerName.MatchString(a.Header.UserHeader) {
		errs = append(errs, fmt.Errorf("auth.header.user_header %q is not a header name", a.Header.UserHeader))
	}

	if a.Header.GroupsHeader != "" && !headerName.MatchString(a.Header.GroupsHeader) {
		errs = append(errs, fmt.Errorf("auth.header.groups_header %q is not a header name", a.Header.GroupsHeader))
	}

	return errs
}
