package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/basepath"
)

func (a *API) registerAuth(api huma.API) {
	huma.Register(api, public(huma.Operation{OperationID: "getAuthState", Method: http.MethodGet, Path: Prefix + "/auth/state", Tags: []string{"auth"}, Summary: "Sign-in state and CSRF token", Description: "Also sets the per-browser CSRF cookie the token is bound to."}),
		func(ctx context.Context, _ *struct{}) (*body[AuthState], error) {
			st, err := a.authState(ctx)

			return &body[AuthState]{Body: st}, a.problemFor(ctx, err)
		})

	huma.Register(api, public(huma.Operation{OperationID: "createAdmin", Method: http.MethodPost, Path: Prefix + "/setup/admin", Tags: []string{"auth"}, Summary: "Create the first admin account", Description: "Only before an admin exists; 409 once one does. The one-time setup token is printed in the server log at startup. Signs in as the new admin.", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				SetupToken string `json:"setup_token" minLength:"1"`
				Username   string `json:"username" minLength:"1" maxLength:"64"`
				Password   string `json:"password" minLength:"1" maxLength:"1024"`
			}
		},
		) (*struct{}, error) {
			ri := info(ctx)
			err := a.auth.CreateAdmin(ctx, ri.w, ri.r, in.Body.SetupToken, in.Body.Username, in.Body.Password)

			return nil, a.authProblem(ctx, err)
		})

	huma.Register(api, public(huma.Operation{OperationID: "login", Method: http.MethodPost, Path: Prefix + "/auth/login", Tags: []string{"auth"}, Summary: "Sign in with a username and password", Description: "Sets the session cookie. 401 on bad credentials, 429 while locked out. With TOTP on, a login without a code answers 401 with `errors[0].location` set to `body.totp`.", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				Username string `json:"username" minLength:"1" maxLength:"64"`
				Password string `json:"password" minLength:"1" maxLength:"1024"`
				TOTP     string `json:"totp,omitempty" maxLength:"16" doc:"The one-time code, when TOTP is enabled"`
			}
		},
		) (*struct{}, error) {
			ri := info(ctx)
			err := a.auth.Login(ctx, ri.w, ri.r, in.Body.Username, in.Body.Password, in.Body.TOTP)

			return nil, a.authProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "logout", Method: http.MethodPost, Path: Prefix + "/auth/logout", Tags: []string{"auth"}, Summary: "Sign out", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			ri := info(ctx)

			return nil, a.problemFor(ctx, a.auth.Logout(ri.w, ri.r))
		})

	huma.Register(api, public(huma.Operation{OperationID: "oidcLogin", Method: http.MethodGet, Path: Prefix + "/auth/oidc/login", Tags: []string{"auth"}, Summary: "Start OpenID Connect sign-in", Description: "Redirects to the OpenID Connect provider.", DefaultStatus: http.StatusFound}),
		func(ctx context.Context, _ *struct{}) (*redirect, error) {
			ri := info(ctx)

			loc, err := a.auth.BeginOIDC(ctx, ri.w, ri.r)
			if err != nil {
				a.log.Warn("OpenID Connect sign-in could not start", "error", err)

				return a.loginError(ctx, err), nil
			}

			return &redirect{Status: http.StatusFound, Location: loc}, nil
		})

	huma.Register(api, public(huma.Operation{OperationID: "oidcCallback", Method: http.MethodGet, Path: Prefix + "/auth/oidc/callback", Tags: []string{"auth"}, Summary: "Finish OpenID Connect sign-in", Description: "Redirects to the web interface, or to its sign-in page with an error parameter.", DefaultStatus: http.StatusFound}),
		func(ctx context.Context, _ *struct {
			Code  string `query:"code"`
			State string `query:"state"`
			Error string `query:"error"`
		},
		) (*redirect, error) {
			ri := info(ctx)

			if err := a.auth.FinishOIDC(ctx, ri.w, ri.r); err != nil {
				a.log.Warn("OpenID Connect sign-in failed", "error", err)

				return a.loginError(ctx, err), nil
			}

			return &redirect{Status: http.StatusFound, Location: basepath.From(ctx)}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "changePassword", Method: http.MethodPut, Path: Prefix + "/auth/password", Tags: []string{"auth"}, Summary: "Change the local admin password", Description: "Signs out every other session. 401 if the current password is wrong.", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				CurrentPassword string `json:"current_password" minLength:"1" maxLength:"1024"`
				NewPassword     string `json:"new_password" minLength:"1" maxLength:"1024"`
			}
		},
		) (*struct{}, error) {
			return nil, a.authProblem(ctx, a.auth.ChangePassword(ctx, info(ctx).r, in.Body.CurrentPassword, in.Body.NewPassword))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "enrollTOTP", Method: http.MethodPost, Path: Prefix + "/auth/totp/enroll", Tags: []string{"auth"}, Summary: "Start TOTP enrolment", Description: "Returns a new secret to add to an authenticator app; it takes effect once confirmed."}),
		func(ctx context.Context, _ *struct{}) (*body[TOTPEnrollment], error) {
			out := &body[TOTPEnrollment]{}

			var err error

			out.Body.Secret, out.Body.OTPAuthURL, err = a.auth.EnrollTOTP(ctx)

			return out, a.authProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "confirmTOTP", Method: http.MethodPost, Path: Prefix + "/auth/totp/confirm", Tags: []string{"auth"}, Summary: "Finish TOTP enrolment with a code", Description: "401 on a wrong code. From then on, a login needs a code.", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				Code string `json:"code" minLength:"6" maxLength:"16"`
			}
		},
		) (*struct{}, error) {
			return nil, a.authProblem(ctx, a.auth.ConfirmTOTP(ctx, in.Body.Code))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "disableTOTP", Method: http.MethodPost, Path: Prefix + "/auth/totp/disable", Tags: []string{"auth"}, Summary: "Turn TOTP off", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				Code string `json:"code" minLength:"6" maxLength:"16"`
			}
		},
		) (*struct{}, error) {
			return nil, a.authProblem(ctx, a.auth.DisableTOTP(ctx, in.Body.Code))
		})
}

func (a *API) authState(ctx context.Context) (AuthState, error) {
	ri := info(ctx)

	settings, err := a.auth.Settings(ctx)
	if err != nil {
		return AuthState{}, err
	}

	setup, err := a.auth.SetupRequired(ctx)
	if err != nil {
		return AuthState{}, err
	}

	st := AuthState{
		// Setup is offered only where its result can be used to sign in.
		SetupRequired: setup && settings.LocalAllowed(),
		Mode:          string(settings.Mode),
		CSRFToken:     a.auth.CSRFToken(ri.w, ri.r),
		LocalLogin:    settings.LocalAllowed(),
	}

	if p := ri.principal; p != nil {
		st.Authenticated = true
		st.User = Some(User{Name: p.Name})
	}

	if settings.OIDCEnabled() {
		u := basepath.From(ctx) + auth.OIDCLoginPath[1:]
		st.OIDCLoginURL = &u
	}

	return st, nil
}

func (a *API) loginError(ctx context.Context, err error) *redirect {
	msg := "Sign-in failed. Check the server log for details."

	switch {
	case errors.Is(err, auth.ErrNotAdmin), errors.Is(err, auth.ErrOIDCDisabled):
		msg = err.Error()
	}

	return &redirect{Status: http.StatusFound, Location: basepath.From(ctx) + "login?error=" + url.QueryEscape(msg)}
}

func (a *API) authProblem(ctx context.Context, err error) error {
	var (
		locked *auth.LockedError
		input  *auth.InputError
	)

	switch {
	case err == nil:
		return nil
	case errors.As(err, &locked):
		secs := int(math.Ceil(locked.RetryAfter.Seconds()))

		return huma.ErrorWithHeaders(huma.Error429TooManyRequests(err.Error()), http.Header{"Retry-After": {strconv.Itoa(secs)}})
	case errors.As(err, &input):
		return fieldError("body."+input.Field, input.Err.Error(), nil)
	case errors.Is(err, auth.ErrBadCredentials), errors.Is(err, auth.ErrBadTOTP):
		return huma.Error401Unauthorized(err.Error())
	case errors.Is(err, auth.ErrTOTPRequired):
		return huma.Error401Unauthorized(err.Error(), &huma.ErrorDetail{Location: "body.totp", Message: "required"})
	case errors.Is(err, auth.ErrAdminExists):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, auth.ErrBadSetupToken), errors.Is(err, auth.ErrLocalDisabled):
		return huma.Error403Forbidden(err.Error())
	}

	return a.problemFor(ctx, err)
}
