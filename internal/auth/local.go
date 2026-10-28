package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

func (s *Service) admin(ctx context.Context) (*admin, error) {
	var a admin

	err := s.db.Settings().Get(ctx, adminSetting, &a)

	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, nil //nolint:nilnil // no admin yet is not an error
	case err != nil:
		return nil, err
	}

	return &a, nil
}

// Admin returns the local admin account, or nil before setup.
func (s *Service) Admin(ctx context.Context) (*Admin, error) {
	a, err := s.admin(ctx)
	if err != nil || a == nil {
		return nil, err
	}

	return &Admin{Username: a.Username, TOTP: a.TOTP}, nil
}

// SetupRequired reports whether the first admin account still has to be created.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	a, err := s.admin(ctx)

	return a == nil && err == nil, err
}

// ValidateUsername checks an admin username.
func ValidateUsername(name string) error {
	if name == "" || len(name) > 64 {
		return errors.New("username must be 1 to 64 characters")
	}

	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("username must not contain spaces or control characters")
		}
	}

	return nil
}

// CreateAdmin creates the first admin account with the setup token from the log, and signs it in.
func (s *Service) CreateAdmin(ctx context.Context, w http.ResponseWriter, r *http.Request, token, username, password string) error {
	key := "setup:" + s.ClientIP(r)
	if d := s.limiter.locked(key, s.now()); d > 0 {
		return &LockedError{RetryAfter: d}
	}

	s.mu.Lock()
	expected := s.setupToken
	s.mu.Unlock()

	existing, err := s.admin(ctx)
	if err != nil {
		return err
	}

	if existing != nil || expected == "" {
		return ErrAdminExists
	}

	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(expected)) != 1 {
		s.limiter.fail(key, 10, 15*time.Minute, s.now())

		return ErrBadSetupToken
	}

	if err := ValidateUsername(username); err != nil {
		return &InputError{Field: "username", Err: err}
	}

	if err := ValidatePassword(password); err != nil {
		return &InputError{Field: "password", Err: err}
	}

	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	if err := s.db.Settings().Set(ctx, adminSetting, admin{Username: username, PasswordHash: hash, CreatedAt: s.now().UTC()}); err != nil {
		return err
	}

	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()

	s.log.Info("admin account created", "username", username)

	return s.StartSession(ctx, w, r, username)
}

// CheckSetupToken reports whether token is the setup token while no admin exists yet, so setup steps that run before the account is created can be authorised. Wrong guesses count towards the same lockout as CreateAdmin.
func (s *Service) CheckSetupToken(r *http.Request, token string) error {
	key := "setup:" + s.ClientIP(r)
	if d := s.limiter.locked(key, s.now()); d > 0 {
		return &LockedError{RetryAfter: d}
	}

	s.mu.Lock()
	expected := s.setupToken
	s.mu.Unlock()

	if expected == "" {
		return ErrAdminExists
	}

	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(expected)) != 1 {
		s.limiter.fail(key, 10, 15*time.Minute, s.now())

		return ErrBadSetupToken
	}

	return nil
}

// Login checks a username, password and, when enrolled, a TOTP code, and signs the browser in. Repeated failures lock the account and the client address out for the lockout window.
func (s *Service) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, username, password, code string) error {
	settings, err := s.settings(ctx)
	if err != nil {
		return err
	}

	if !settings.LocalAllowed() {
		return ErrLocalDisabled
	}

	now := s.now()
	userKey := "user:" + strings.ToLower(username)
	ipKey := "ip:" + s.ClientIP(r)

	if d := max(s.limiter.locked(userKey, now), s.limiter.locked(ipKey, now)); d > 0 {
		return &LockedError{RetryAfter: d}
	}

	fail := func(cause error) error {
		lockedUser := s.limiter.fail(userKey, settings.LockoutAttempts, settings.LockoutWindow, now)
		lockedIP := s.limiter.fail(ipKey, settings.LockoutAttempts*ipAttemptFactor, settings.LockoutWindow, now)

		if lockedUser || lockedIP {
			s.log.Warn("sign-in locked out after repeated failures", "username", username, "client", s.ClientIP(r))

			return &LockedError{RetryAfter: settings.LockoutWindow}
		}

		return cause
	}

	a, err := s.admin(ctx)
	if err != nil {
		return err
	}

	hash, name := dummyHash(), ""
	if a != nil {
		hash, name = a.PasswordHash, a.Username
	}

	ok, err := VerifyPassword(hash, password)
	if err != nil {
		return err
	}

	if !ok || a == nil || subtle.ConstantTimeCompare([]byte(username), []byte(name)) != 1 {
		return fail(ErrBadCredentials)
	}

	if a.TOTP {
		if code == "" {
			// The password was right; asking for the code does not count as a failure.
			return ErrTOTPRequired
		}

		if err := s.checkTOTP(ctx, totpRef, code); err != nil {
			return fail(err)
		}
	}

	s.limiter.reset(userKey)
	s.limiter.reset(ipKey)

	return s.StartSession(ctx, w, r, a.Username)
}

// ChangePassword replaces the admin password after checking the current one, and signs out every other session.
func (s *Service) ChangePassword(ctx context.Context, r *http.Request, current, next string) error {
	a, err := s.admin(ctx)
	if err != nil {
		return err
	}

	if a == nil {
		return ErrBadCredentials
	}

	if ok, err := VerifyPassword(a.PasswordHash, current); err != nil || !ok {
		return ErrBadCredentials
	}

	if err := ValidatePassword(next); err != nil {
		return &InputError{Field: "new_password", Err: err}
	}

	if a.PasswordHash, err = HashPassword(next); err != nil {
		return err
	}

	if err := s.db.Settings().Set(ctx, adminSetting, a); err != nil {
		return err
	}

	return s.EndOtherSessions(ctx, r)
}

// EnrollTOTP starts TOTP enrolment and returns the secret and its otpauth:// URL for an authenticator app. It takes effect once ConfirmTOTP accepts a code.
func (s *Service) EnrollTOTP(ctx context.Context) (secret, url string, err error) {
	a, err := s.admin(ctx)
	if err != nil {
		return "", "", err
	}

	if a == nil {
		return "", "", errors.New("create the admin account first")
	}

	secret, url, err = newTOTPKey(a.Username, rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generating TOTP secret: %w", err)
	}

	if err := s.secrets.Put(ctx, totpPendingRef, []byte(secret)); err != nil {
		return "", "", err
	}

	return secret, url, nil
}

// ConfirmTOTP finishes enrolment with a code from the authenticator app.
func (s *Service) ConfirmTOTP(ctx context.Context, code string) error {
	if err := s.checkTOTP(ctx, totpPendingRef, code); err != nil {
		return err
	}

	secret, err := s.secrets.Get(ctx, totpPendingRef)
	if err != nil {
		return err
	}
	defer clear(secret)

	if err := s.secrets.Put(ctx, totpRef, secret); err != nil {
		return err
	}

	_ = s.secrets.Delete(ctx, totpPendingRef)

	return s.setTOTP(ctx, true)
}

// DisableTOTP turns TOTP off after checking a current code.
func (s *Service) DisableTOTP(ctx context.Context, code string) error {
	if err := s.checkTOTP(ctx, totpRef, code); err != nil {
		return err
	}

	if err := s.secrets.Delete(ctx, totpRef); err != nil {
		return err
	}

	return s.setTOTP(ctx, false)
}

func (s *Service) setTOTP(ctx context.Context, on bool) error {
	a, err := s.admin(ctx)
	if err != nil {
		return err
	}

	if a == nil {
		return errors.New("create the admin account first")
	}

	a.TOTP = on

	return s.db.Settings().Set(ctx, adminSetting, a)
}

// checkTOTP validates code against the secret stored under ref, allowing one step of clock skew and rejecting a code that was already used.
func (s *Service) checkTOTP(ctx context.Context, ref, code string) error {
	secret, err := s.secrets.Get(ctx, ref)
	if errors.Is(err, secrets.ErrNotFound) {
		return ErrBadTOTP
	}

	if err != nil {
		return err
	}
	defer clear(secret)

	step, ok := totpStep(string(secret), code, s.now())
	if !ok {
		return ErrBadTOTP
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if step <= s.lastTOTP {
		return ErrBadTOTP
	}

	s.lastTOTP = step

	return nil
}

// newTOTPKey makes a random secret for account and its otpauth:// URL.
func newTOTPKey(account string, r io.Reader) (secret, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Encore", AccountName: account, Rand: r})
	if err != nil {
		return "", "", err
	}

	return key.Secret(), key.URL(), nil
}

var totpOpts = totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// totpCode returns the code for the base32 secret at t.
func totpCode(secret string, t time.Time) (string, error) {
	return totp.GenerateCodeCustom(secret, t, totpOpts)
}

// totpStep reports whether code is valid for secret at now, allowing one step of clock skew, and returns the time step it matched so the caller can reject a replay.
func totpStep(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)

	ok, err := totp.ValidateCustom(code, secret, now, totpOpts)
	if err != nil || !ok {
		return 0, false
	}

	// Find which step matched, so the same code cannot be replayed within its window.
	step := int64(0)

	for _, offset := range []int64{0, -1, 1} {
		t := now.Add(time.Duration(offset*30) * time.Second)
		if want, err := totpCode(secret, t); err == nil && want == code {
			step = t.Unix() / 30

			break
		}
	}

	return step, true
}
