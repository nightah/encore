package smapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

var (
	fastRetry = smapi.RetryPolicy{Base: time.Millisecond, Max: 2 * time.Millisecond, MaxRetryAfter: time.Millisecond}
	fastPoll  = smapi.PollConfig{Interval: time.Millisecond, MaxInterval: 2 * time.Millisecond, Timeout: 5 * time.Second}
)

func newClient(t *testing.T, fake *smapitest.Server) *smapi.Client {
	t.Helper()

	opts := fake.Options()

	lc, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: opts.ClientID, ClientSecret: opts.ClientSecret},
		Endpoint:    fake.Endpoint(),
	})
	require.NoError(t, err)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: opts.RefreshToken}))

	c, err := smapi.New(lc.TokenSource(store), smapi.Options{BaseURL: fake.URL, Retry: fastRetry, Poll: fastPoll})
	require.NoError(t, err)

	return c
}

func TestVendors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	vendors, err := c.Vendors(t.Context())
	require.NoError(t, err)
	require.Len(t, vendors, 1)
	assert.Equal(t, smapitest.DefaultVendorID, vendors[0].ID)
	assert.Equal(t, []string{"ROLE_ADMINISTRATOR"}, vendors[0].Roles)
	assert.Equal(t, 1, fake.Refreshes())
}

func TestListSkillsPaginates(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	for i := range 120 {
		fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-US": fmt.Sprintf("Skill %d", i)})
	}

	fake.AddSkill("OTHER", map[string]string{"en-US": "Not mine"})

	skills, err := newClient(t, fake).ListSkills(t.Context(), smapitest.DefaultVendorID)
	require.NoError(t, err)
	require.Len(t, skills, 120)
	assert.Equal(t, "Skill 119", skills[119].NameByLocale["en-US"])
	assert.Equal(t, 3, fake.Calls("GET /v1/skills"))
}

func TestRetriesThrottledRequests(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	fake.Inject(http.StatusTooManyRequests, 2)
	fake.Inject(http.StatusServiceUnavailable, 1)

	_, err := c.Vendors(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, fake.Injected())
}

func TestRetriesGiveUp(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	fake.Inject(http.StatusTooManyRequests, 10)

	_, err := c.Vendors(t.Context())

	var se *smapi.Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusTooManyRequests, se.StatusCode)
	assert.Equal(t, "injected failure", se.Message)
	assert.Equal(t, 6, fake.Injected())
}

func TestPOSTIsRetriedOnThrottle(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	fake.Inject(http.StatusTooManyRequests, 1)

	up, err := c.CreateUpload(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, up.URL)
	assert.Equal(t, 1, fake.Calls("POST /v1/skills/uploads"))
}

func TestErrorBody(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})

	_, err := newClient(t, fake).ImportStatus(t.Context(), "nope")

	var se *smapi.Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))
	assert.Equal(t, "get import status", se.Op)
	assert.Contains(t, err.Error(), "import nope not found")
}

func TestErrorString(t *testing.T) {
	err := &smapi.Error{Op: "import", StatusCode: 400, Message: "Bad", Violations: []smapi.Problem{{Code: "A", Message: "one"}, {Message: "two"}}}
	assert.Equal(t, "smapi: import: HTTP 400: Bad (A: one; two)", err.Error())
	assert.Equal(t, 0, smapi.StatusCode(errors.New("other")))
}

func TestPutPackageCarriesNoBearer(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	up, err := c.CreateUpload(t.Context())
	require.NoError(t, err)
	require.NoError(t, c.PutPackage(t.Context(), up.URL, []byte("zip")))
	assert.Equal(t, 1, fake.Calls("PUT /s3/{id}"))
}

func TestPutPackageErrorOmitsPresignedQuery(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	// Nothing listens on port 1, so the request fails in the transport, whose error quotes the URL.
	err := c.PutPackage(t.Context(), "http://127.0.0.1:1/upload?X-Amz-Signature=presigned-secret", []byte("zip"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "presigned-secret")
	assert.Contains(t, err.Error(), "127.0.0.1:1/upload")
}

func TestTokenRefreshedWhenShortLived(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{TokenTTL: 5 * time.Second})
	c := newClient(t, fake)

	for range 3 {
		_, err := c.Vendors(t.Context())
		require.NoError(t, err)
	}

	assert.Equal(t, 3, fake.Refreshes())
}

func TestRevokedGrantSurfacesReauthorise(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	fake.RevokeRefreshToken()

	_, err := newClient(t, fake).Vendors(t.Context())
	require.ErrorIs(t, err, lwa.ErrReauthorise)
}

func TestPollTimeout(t *testing.T) {
	calls := 0
	err := smapi.Poll(t.Context(), smapi.PollConfig{Interval: time.Millisecond, Timeout: 30 * time.Millisecond}, func(context.Context) (bool, error) {
		calls++

		return false, nil
	})
	require.ErrorIs(t, err, smapi.ErrPollTimeout)
	assert.Greater(t, calls, 1)
}

func TestPollStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	err := smapi.Poll(t.Context(), fastPoll, func(context.Context) (bool, error) { return false, boom })
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, smapi.ErrPollTimeout)
}

func TestNewRequiresTokenSource(t *testing.T) {
	_, err := smapi.New(nil, smapi.Options{})
	require.Error(t, err)
}
