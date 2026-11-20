package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateUsername(t *testing.T) {
	testCases := []struct {
		name string
		have string
		err  bool
	}{
		{name: "ShouldAcceptAPlainName", have: "admin"},
		{name: "ShouldAcceptOneCharacter", have: "a"},
		{name: "ShouldAcceptAnEmailAddress", have: "household.admin@example.com"},
		{name: "ShouldAcceptAccents", have: "Zoë"},
		{name: "ShouldAccept64Characters", have: strings.Repeat("a", 64)},
		{name: "ShouldAccept64MultiByteCharacters", have: strings.Repeat("ü", 64)},
		{name: "ShouldAcceptCJKCharacters", have: strings.Repeat("管理", 20)},
		{name: "ShouldRejectEmpty", have: "", err: true},
		{name: "ShouldReject65Characters", have: strings.Repeat("a", 65), err: true},
		{name: "ShouldReject65MultiByteCharacters", have: strings.Repeat("ü", 65), err: true},
		{name: "ShouldRejectASpace", have: "house hold", err: true},
		{name: "ShouldRejectATrailingTab", have: "admin\t", err: true},
		{name: "ShouldRejectALeadingNewline", have: "\nadmin", err: true},
		{name: "ShouldRejectANulByte", have: "ad\x00min", err: true},
		{name: "ShouldRejectANonBreakingSpace", have: "ad min", err: true},
		{name: "ShouldRejectATrailingLineSeparator", have: "admin ", err: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err {
				require.Error(t, ValidateUsername(tc.have))
			} else {
				require.NoError(t, ValidateUsername(tc.have))
			}
		})
	}
}

func TestAdminBeforeAndAfterSetup(t *testing.T) {
	f := newFixture(t, nil)

	a, err := f.svc.Admin(t.Context())
	require.NoError(t, err)
	assert.Nil(t, a, "no admin before setup")

	f.setup(t)

	a, err = f.svc.Admin(t.Context())
	require.NoError(t, err)
	assert.Equal(t, &Admin{Username: "admin"}, a)
}

func TestCreateAdminValidatesInput(t *testing.T) {
	f := newFixture(t, nil)
	token := f.svc.setupToken

	var input *InputError
	require.ErrorAs(t, f.svc.CreateAdmin(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), token, "house hold", "correct horse battery"), &input)
	assert.Equal(t, "username", input.Field)

	required, err := f.svc.SetupRequired(t.Context())
	require.NoError(t, err)
	assert.True(t, required, "a rejected request creates nothing")

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.CreateAdmin(t.Context(), rec, request(http.MethodPost, "/"), "  "+token+"\n", "admin", "correct horse battery"), "whitespace around a pasted token is ignored")
	require.NotNil(t, cookie(rec, SessionCookie), "the new admin is signed in")
	assert.Contains(t, f.log.String(), "admin account created")

	require.ErrorIs(t, f.svc.CheckSetupToken(request(http.MethodPost, "/"), token), ErrAdminExists, "the token is single-use")
}

func TestCreateAdminNeverReplacesAnExistingAccount(t *testing.T) {
	f := newFixture(t, nil)
	token := f.svc.setupToken

	// Another Encore, or a restored database, created the account after this one generated its setup token.
	require.NoError(t, f.db.Settings().Set(t.Context(), adminSetting, admin{Username: "owner", PasswordHash: HashPassword("the real admin's password")}))

	rec := httptest.NewRecorder()
	require.ErrorIs(t, f.svc.CreateAdmin(t.Context(), rec, request(http.MethodPost, "/"), token, "intruder", "correct horse battery"), ErrAdminExists)
	assert.Nil(t, cookie(rec, SessionCookie))

	a, err := f.svc.Admin(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "owner", a.Username, "the setup token cannot overwrite an account")
}

func TestSetupTokenLockout(t *testing.T) {
	f := newFixture(t, nil)
	token := f.svc.setupToken

	r := request(http.MethodPost, "/")
	require.NoError(t, f.svc.CheckSetupToken(r, token))

	for range 9 {
		require.ErrorIs(t, f.svc.CheckSetupToken(r, "guess"), ErrBadSetupToken)
	}

	// The tenth wrong guess, through either entry point, locks the address.
	require.ErrorIs(t, f.svc.CreateAdmin(t.Context(), httptest.NewRecorder(), r, "guess", "admin", "correct horse battery"), ErrBadSetupToken)

	var locked *LockedError
	require.ErrorAs(t, f.svc.CheckSetupToken(r, token), &locked, "even the right token is refused while locked")
	assert.Equal(t, 15*time.Minute, locked.RetryAfter)
	require.ErrorAs(t, f.svc.CreateAdmin(t.Context(), httptest.NewRecorder(), r, token, "admin", "correct horse battery"), &locked)

	other := request(http.MethodPost, "/")
	other.RemoteAddr = "198.51.100.7:1234"
	require.NoError(t, f.svc.CheckSetupToken(other, token), "another address is not locked")

	f.advance(15 * time.Minute)
	require.NoError(t, f.svc.CheckSetupToken(r, token))
}

func TestLoginWithoutAnAdmin(t *testing.T) {
	f := newFixture(t, nil)

	err := f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "", "encore-dummy-password", "")
	require.ErrorIs(t, err, ErrBadCredentials, "the dummy hash never signs anyone in")
}

func TestLoginSuccessResetsFailures(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	login := func(password string) error {
		return f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", password, "")
	}

	for range 2 {
		require.ErrorIs(t, login("wrong password"), ErrBadCredentials)
	}

	require.NoError(t, login("correct horse battery"))

	for range 2 {
		require.ErrorIs(t, login("wrong password"), ErrBadCredentials, "earlier failures were forgiven by the success")
	}
}

func TestLoginUsernameIsExact(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldRejectADifferentCase", have: "Admin"},
		{name: "ShouldRejectATrailingSpace", have: "admin "},
		{name: "ShouldRejectAPrefix", have: "adm"},
		{name: "ShouldRejectALongerName", have: "admin2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), tc.have, "correct horse battery", "")
			require.ErrorIs(t, err, ErrBadCredentials)

			f.advance(16 * time.Minute)
		})
	}
}

func TestLoginReportsAStoredHashItCannotRead(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)

	a, err := f.svc.admin(t.Context())
	require.NoError(t, err)

	a.PasswordHash = "$2a$10$bcrypt"
	require.NoError(t, f.db.Settings().Set(t.Context(), adminSetting, a))

	err = f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", "")
	require.ErrorIs(t, err, errMalformedHash)
}

func TestLoginReportsStorageErrors(t *testing.T) {
	f := newFixture(t, nil)
	f.setup(t)
	require.NoError(t, f.db.Close())

	err := f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", "correct horse battery", "")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrBadCredentials, "a broken database is not a wrong password")

	err = f.svc.CreateAdmin(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "token", "admin", "correct horse battery")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAdminExists)

	require.Error(t, f.svc.ChangePassword(t.Context(), request(http.MethodPost, "/"), "correct horse battery", "a new long password"))
}

func TestChangePassword(t *testing.T) {
	f := newFixture(t, nil)

	require.ErrorIs(t, f.svc.ChangePassword(t.Context(), request(http.MethodPost, "/"), "x", "a new long password"), ErrBadCredentials, "there is no account yet")

	current := f.setup(t)

	rec := httptest.NewRecorder()
	require.NoError(t, f.svc.Login(t.Context(), rec, request(http.MethodPost, "/"), "admin", "correct horse battery", ""))
	other := cookie(rec, SessionCookie)

	r := request(http.MethodPost, "/", current)

	require.ErrorIs(t, f.svc.ChangePassword(t.Context(), r, "wrong password", "a new long password"), ErrBadCredentials)

	var input *InputError
	require.ErrorAs(t, f.svc.ChangePassword(t.Context(), r, "correct horse battery", "short"), &input)
	assert.Equal(t, "new_password", input.Field)

	require.NoError(t, f.svc.ChangePassword(t.Context(), r, "correct horse battery", "a new long password"))

	login := func(password string) error {
		return f.svc.Login(t.Context(), httptest.NewRecorder(), request(http.MethodPost, "/"), "admin", password, "")
	}

	require.ErrorIs(t, login("correct horse battery"), ErrBadCredentials)
	require.NoError(t, login("a new long password"))

	p, err := f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", current))
	require.NoError(t, err)
	assert.NotNil(t, p, "the session that changed the password stays signed in")

	p, err = f.svc.Authenticate(httptest.NewRecorder(), request(http.MethodGet, "/", other))
	require.NoError(t, err)
	assert.Nil(t, p, "every other session is signed out")
}
