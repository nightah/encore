package smapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

// packageZip builds a minimal skill package the fake accepts, with an interaction model per locale.
func packageZip(t *testing.T, invocation string, locales ...string) []byte {
	t.Helper()

	type info struct {
		Name string `json:"name"`
	}

	names := map[string]info{}
	for _, l := range locales {
		names[l] = info{Name: "Encore"}
	}

	manifest, err := json.Marshal(map[string]any{"manifest": map[string]any{"publishingInformation": map[string]any{"locales": names}}})
	require.NoError(t, err)

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	write := func(name string, b []byte) {
		w, err := zw.Create(name)
		require.NoError(t, err)

		_, err = w.Write(b)
		require.NoError(t, err)
	}

	write("skill-package/skill.json", manifest)

	for _, l := range locales {
		write("skill-package/interactionModels/custom/"+l+".json", []byte(`{"interactionModel":{"languageModel":{"invocationName":"`+invocation+`"}}}`))
	}

	require.NoError(t, zw.Close())

	return buf.Bytes()
}

// upload puts pkg at a fresh presigned URL and returns that URL.
func upload(t *testing.T, c *smapi.Client, pkg []byte) string {
	t.Helper()

	up, err := c.CreateUpload(t.Context())
	require.NoError(t, err)
	require.NoError(t, c.PutPackage(t.Context(), up.URL, pkg))

	return up.URL
}

// stubClient returns a client for a server answering every SMAPI request with h, authorised by a static token.
func stubClient(t *testing.T, h http.HandlerFunc) *smapi.Client {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := smapi.New(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "static"}), smapi.Options{BaseURL: srv.URL, Retry: fastRetry, Poll: fastPoll})
	require.NoError(t, err)

	return c
}

func TestImportNewCreatesSkillAndReportsStatus(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	id, err := c.ImportNew(t.Context(), smapitest.DefaultVendorID, upload(t, c, packageZip(t, "encore", "en-US", "en-AU")))
	require.NoError(t, err)
	assert.NotContains(t, id, "/", "the import ID is the Location header's last segment")

	st, err := c.ImportStatus(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, smapi.StatusSucceeded, st.Status)
	assert.NotEmpty(t, st.Skill.ID)
	assert.Equal(t, "etag-"+id, st.Skill.ETag)
	require.Len(t, st.Skill.Resources, 3)
	assert.Equal(t, "Manifest", st.Skill.Resources[0].Name)

	sk, ok := fake.Skill(st.Skill.ID)
	require.True(t, ok)
	assert.Equal(t, smapitest.DefaultVendorID, sk.VendorID)
	assert.Equal(t, map[string]string{"en-US": "Encore", "en-AU": "Encore"}, sk.Names)
}

func TestImportExistingUpdatesSkill(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	skillID := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-US": "Old"})

	id, err := c.ImportExisting(t.Context(), skillID, upload(t, c, packageZip(t, "encore", "en-US")), "etag-1")
	require.NoError(t, err)

	st, err := c.ImportStatus(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, skillID, st.Skill.ID)

	sk, _ := fake.Skill(skillID)
	assert.Equal(t, 1, sk.Imports)
	assert.Equal(t, "Encore", sk.Names["en-US"])
	assert.Equal(t, 1, fake.SkillCount(), "an existing skill is updated, not duplicated")
}

func TestImportExistingSendsIfMatchOnlyWithETag(t *testing.T) {
	for name, tc := range map[string]struct {
		eTag string
		want []string
	}{
		"with an eTag":    {eTag: "abc", want: []string{"abc"}},
		"without an eTag": {eTag: "", want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string

			c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Values("If-Match")

				w.Header().Set("Location", "/v1/skills/imports/imp-1")
				w.WriteHeader(http.StatusAccepted)
			})

			id, err := c.ImportExisting(t.Context(), "skill", "https://s3.example/x", tc.eTag)
			require.NoError(t, err)
			assert.Equal(t, "imp-1", id)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestImportErrors(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	_, err := c.ImportExisting(t.Context(), "amzn1.ask.skill.missing", upload(t, c, packageZip(t, "encore", "en-US")), "")
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))

	_, err = c.ImportNew(t.Context(), smapitest.DefaultVendorID, "https://elsewhere.example/never-uploaded")
	assert.Equal(t, http.StatusBadRequest, smapi.StatusCode(err))
	assert.Equal(t, 0, fake.SkillCount())
}

func TestImportWithoutLocationHeaderFails(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })

	_, err := c.ImportNew(t.Context(), "V", "https://s3.example/x")
	require.ErrorContains(t, err, "import new skill: response has no Location header")
}

func TestImportStatusReportsFailedLocale(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	fake.FailLocale("en-AU", "sample utterance conflict")
	c := newClient(t, fake)

	id, err := c.ImportNew(t.Context(), smapitest.DefaultVendorID, upload(t, c, packageZip(t, "encore", "en-US", "en-AU")))
	require.NoError(t, err)

	st, err := c.ImportStatus(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, smapi.StatusFailed, st.Status)

	var failed []smapi.ResourceStatus

	for _, r := range st.Skill.Resources {
		if r.Status == smapi.StatusFailed {
			failed = append(failed, r)
		}
	}

	require.Len(t, failed, 1)
	assert.Equal(t, "InteractionModel.en-AU", failed[0].Name)
	assert.Equal(t, []smapi.Problem{{Code: "INVALID_INTERACTION_MODEL", Message: "sample utterance conflict"}}, failed[0].Errors)
}

func TestSkillStatusReportsModelBuilds(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	fake.FailLocale("en-AU", "bad slot")
	c := newClient(t, fake)

	id, err := c.ImportNew(t.Context(), smapitest.DefaultVendorID, upload(t, c, packageZip(t, "encore", "en-US", "en-AU")))
	require.NoError(t, err)

	imp, err := c.ImportStatus(t.Context(), id)
	require.NoError(t, err)

	st, err := c.SkillStatus(t.Context(), imp.Skill.ID)
	require.NoError(t, err)
	assert.Nil(t, st.Manifest, "only interaction model builds are asked for")
	require.Len(t, st.InteractionModel, 2)
	assert.Equal(t, smapi.StatusSucceeded, st.InteractionModel["en-US"].LastUpdateRequest.Status)

	au := st.InteractionModel["en-AU"].LastUpdateRequest
	assert.Equal(t, smapi.StatusFailed, au.Status)
	assert.Equal(t, []smapi.Problem{{Code: "INVALID_INTERACTION_MODEL", Message: "bad slot"}}, au.Problems(), "a problem on both the build and its step is reported once")
	assert.True(t, json.Valid(st.Raw))
	assert.Contains(t, string(st.Raw), "en-AU")
}

func TestSkillStatusErrors(t *testing.T) {
	_, err := newClient(t, smapitest.New(t, smapitest.Options{})).SkillStatus(t.Context(), "amzn1.ask.skill.missing")
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))

	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"interactionModel":[]}`)) })
	_, err = c.SkillStatus(t.Context(), "skill")
	require.ErrorContains(t, err, "get skill status: decoding response")
}

func TestBuildStatusProblems(t *testing.T) {
	a, b, c := smapi.Problem{Code: "A", Message: "a"}, smapi.Problem{Code: "B", Message: "b"}, smapi.Problem{Message: "c"}

	for name, tc := range map[string]struct {
		status smapi.BuildStatus
		want   []smapi.Problem
	}{
		"none":                  {status: smapi.BuildStatus{}, want: []smapi.Problem{}},
		"top level only":        {status: smapi.BuildStatus{Errors: []smapi.Problem{a}}, want: []smapi.Problem{a}},
		"steps only":            {status: buildWithSteps(nil, []smapi.Problem{b}, []smapi.Problem{c}), want: []smapi.Problem{b, c}},
		"duplicates collapse":   {status: buildWithSteps([]smapi.Problem{a}, []smapi.Problem{a, b}, []smapi.Problem{b}), want: []smapi.Problem{a, b}},
		"same code, other text": {status: buildWithSteps([]smapi.Problem{a}, []smapi.Problem{{Code: "A", Message: "other"}}), want: []smapi.Problem{a, {Code: "A", Message: "other"}}},
	} {
		t.Run(name, func(t *testing.T) {
			got := tc.status.Problems()
			assert.ElementsMatch(t, tc.want, got)
			assert.Len(t, got, len(tc.want))
		})
	}

	errs := make([]smapi.Problem, 1, 4)
	errs[0] = a
	_ = buildWithSteps(errs, []smapi.Problem{b}).Problems()
	assert.Equal(t, smapi.Problem{}, errs[:2][1], "Problems does not append into the build's own slice")
}

func buildWithSteps(top []smapi.Problem, steps ...[]smapi.Problem) smapi.BuildStatus {
	b := smapi.BuildStatus{Errors: top, BuildDetails: &struct {
		Steps []smapi.BuildStep `json:"steps"`
	}{}}

	for _, s := range steps {
		b.BuildDetails.Steps = append(b.BuildDetails.Steps, smapi.BuildStep{Status: smapi.StatusFailed, Errors: s})
	}

	return b
}

func TestEnableDevelopment(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)
	id := fake.AddSkill(smapitest.DefaultVendorID, nil)

	require.NoError(t, c.EnableDevelopment(t.Context(), id))

	sk, _ := fake.Skill(id)
	assert.True(t, sk.Enabled)

	err := c.EnableDevelopment(t.Context(), "amzn1.ask.skill.missing")
	assert.Equal(t, http.StatusNotFound, smapi.StatusCode(err))
}

func TestCreateUploadRequiresURL(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"expiresAt":"1793002725519"}`)) })

	_, err := c.CreateUpload(t.Context())
	require.ErrorContains(t, err, "response has no uploadUrl")
}

func TestPutPackageReportsS3Rejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "<Error><Code>SignatureDoesNotMatch</Code></Error>", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	c := newClient(t, smapitest.New(t, smapitest.Options{}))

	err := c.PutPackage(t.Context(), srv.URL+"/upload?sig=x", []byte("zip"))

	var se *smapi.Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "upload package", se.Op)
	assert.Equal(t, http.StatusForbidden, se.StatusCode)
	assert.Equal(t, "Forbidden", se.Message, "a non-JSON body falls back to the status text")
}

func TestPutPackageRejectsMalformedURL(t *testing.T) {
	c := newClient(t, smapitest.New(t, smapitest.Options{}))

	err := c.PutPackage(t.Context(), "http://exa mple.com/upload?sig=secret", []byte("zip"))
	require.ErrorContains(t, err, "smapi: upload package")
	assert.NotContains(t, err.Error(), "secret")
}

func TestListSkillsStopsOnError(t *testing.T) {
	fake := smapitest.New(t, smapitest.Options{})
	c := newClient(t, fake)

	_, err := c.ListSkills(t.Context(), "")
	assert.Equal(t, http.StatusBadRequest, smapi.StatusCode(err))
}

func TestListSkillsEmptyVendor(t *testing.T) {
	skills, err := newClient(t, smapitest.New(t, smapitest.Options{})).ListSkills(t.Context(), "NOBODY")
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestListSkillsTruncatedWithoutTokenStops(t *testing.T) {
	calls := 0
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++

		_, _ = w.Write([]byte(`{"skills":[{"skillId":"a"}],"isTruncated":true}`))
	})

	skills, err := c.ListSkills(t.Context(), "V")
	require.NoError(t, err)
	assert.Len(t, skills, 1)
	assert.Equal(t, 1, calls, "a truncated page without a next token cannot be followed")
}

func TestVendorsDecodeError(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not json`)) })

	_, err := c.Vendors(t.Context())
	require.ErrorContains(t, err, "smapi: list vendors: decoding response")
}

func TestErrorDecodesViolations(t *testing.T) {
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"INVALID","message":"Bad manifest","violations":[{"code":"V1","message":"name too long"}]}`))
	})

	_, err := c.Vendors(t.Context())

	var se *smapi.Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "INVALID", se.Code)
	assert.Equal(t, []smapi.Problem{{Code: "V1", Message: "name too long"}}, se.Violations)
	assert.Equal(t, "smapi: list vendors: HTTP 400: Bad manifest (V1: name too long)", err.Error())
}

func TestStaticTokenSourceAuthorises(t *testing.T) {
	var auth string

	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")

		_, _ = w.Write([]byte(`{"vendors":[]}`))
	})

	_, err := c.Vendors(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bearer static", auth)
}

type failingTokens struct{}

func (failingTokens) Token() (*oauth2.Token, error) { return nil, context.DeadlineExceeded }

func TestTokenErrorStopsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request may be sent without a token")
	}))
	t.Cleanup(srv.Close)

	c, err := smapi.New(failingTokens{}, smapi.Options{BaseURL: srv.URL})
	require.NoError(t, err)

	_, err = c.Vendors(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "getting access token")
}

// dropConnection closes the connection without a response, as a failing network does.
func dropConnection(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	conn, _, err := http.NewResponseController(w).Hijack()
	if assert.NoError(t, err) {
		assert.NoError(t, conn.Close())
	}
}

func TestGETRetriedAfterTransportError(t *testing.T) {
	calls := 0
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			dropConnection(t, w)

			return
		}

		_, _ = w.Write([]byte(`{"vendors":[{"id":"V"}]}`))
	})

	vendors, err := c.Vendors(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []smapi.Vendor{{ID: "V"}}, vendors)
	assert.Equal(t, 2, calls)
}

func TestPOSTNotRetriedAfterTransportError(t *testing.T) {
	calls := 0
	c := stubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++

		dropConnection(t, w)
	})

	_, err := c.CreateUpload(t.Context())
	require.Error(t, err)
	assert.Equal(t, 1, calls, "a POST may have been processed, so it is not resent")
}

func TestCancelledContextStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		cancel()
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	c, err := smapi.New(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"}), smapi.Options{BaseURL: srv.URL, Retry: smapi.RetryPolicy{MaxRetryAfter: time.Hour}})
	require.NoError(t, err)

	_, err = c.Vendors(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestAPIIsAuthorised(t *testing.T) {
	var auth string

	c := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusNoContent)
	})

	resp, err := c.API().GetVendorListV1(t.Context())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "Bearer static", auth)
}

func TestClientPollUsesClientConfig(t *testing.T) {
	c := newClient(t, smapitest.New(t, smapitest.Options{}))

	calls := 0
	err := c.Poll(t.Context(), func(context.Context) (bool, error) {
		calls++

		return calls == 3, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestPollRespectsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	err := smapi.Poll(ctx, smapi.PollConfig{Interval: time.Hour}, func(context.Context) (bool, error) {
		cancel()

		return false, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, smapi.ErrPollTimeout)
}

func TestPollCheckErrorAfterTimeoutWrapsBoth(t *testing.T) {
	boom := errors.New("boom")

	err := smapi.Poll(t.Context(), smapi.PollConfig{Interval: time.Millisecond, Timeout: time.Millisecond}, func(ctx context.Context) (bool, error) {
		<-ctx.Done()

		return false, boom
	})
	require.ErrorIs(t, err, smapi.ErrPollTimeout)
	require.ErrorIs(t, err, boom)
}
