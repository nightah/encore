package deploy_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

func TestTransient(t *testing.T) {
	t.Parallel()

	unexplained := []smapi.Problem{{Message: "build failed without details"}}
	unreported := []smapi.Problem{{Message: "Amazon reported no build for this locale"}}
	named := []smapi.Problem{{Code: "INVALID_SLOT_VALUE", Message: "slot value 'Dune' is invalid"}}
	network := &url.Error{Op: "Get", URL: "https://api.amazonalexa.com/v1/vendors", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}

	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"none":                       {err: nil, want: false},
		"reauthorise":                {err: fmt.Errorf("smapi: getting access token: %w", lwa.ErrReauthorise), want: false},
		"client rejected":            {err: lwa.ErrInvalidClient, want: false},
		"never authorised":           {err: lwa.ErrNotAuthorised, want: false},
		"poll timeout":               {err: fmt.Errorf("%w: %w", smapi.ErrPollTimeout, context.DeadlineExceeded), want: true},
		"interrupted":                {err: context.Canceled, want: true},
		"build already running":      {err: &smapi.Error{Op: "import skill package", StatusCode: http.StatusConflict}, want: true},
		"throttled":                  {err: &smapi.Error{Op: "list vendors", StatusCode: http.StatusTooManyRequests}, want: true},
		"unavailable":                {err: fmt.Errorf("wrapped: %w", &smapi.Error{Op: "list vendors", StatusCode: http.StatusServiceUnavailable}), want: true},
		"validation":                 {err: &smapi.Error{Op: "import skill package", StatusCode: http.StatusBadRequest}, want: false},
		"unauthorised after refresh": {err: &smapi.Error{Op: "list vendors", StatusCode: http.StatusUnauthorized}, want: false},
		"network":                    {err: fmt.Errorf("smapi: list vendors: %w", network), want: true},
		"build without details":      {err: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": unexplained, "en-GB": unreported}}, want: true},
		"named build error":          {err: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": unexplained, "en-GB": named}}, want: false},
		"coded unexplained message":  {err: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": {{Code: "X", Message: "build failed without details"}}}}, want: false},
		"import errors":              {err: &deploy.Error{Step: deploy.StepImport, Problems: named}, want: false},
		"no details at all":          {err: &deploy.Error{Step: deploy.StepImport}, want: false},
		"vendor choice":              {err: &deploy.VendorChoiceError{}, want: false},
		"package too large":          {err: errors.New("skillpkg: locale en-US: model is 1600000 bytes, over Amazon's limit"), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, deploy.Transient(tc.err), "%v", tc.err)
		})
	}
}

func TestTransientDeployFailures(t *testing.T) {
	t.Run("build without details", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{ImportWithoutModels: true})
		h.override("GET /v1/skills/{id}/status", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, `{"interactionModel":{"en-US":{"lastUpdateRequest":{"status":"FAILED"}},"en-GB":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}}`)
		})

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.True(t, deploy.Transient(err), "%v", err)
	})

	t.Run("build already running", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.override("POST /v1/skills/imports", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusConflict, `{"message":"Another build is in progress"}`)
		})

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.True(t, deploy.Transient(err), "%v", err)
	})

	t.Run("named build error", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.fake.FailLocale("en-GB", "Sample utterance conflicts with another intent")
		h.fake.FailLocale("en-US", "Sample utterance conflicts with another intent")

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.ErrorIs(t, err, deploy.ErrBuildFailed)
		assert.False(t, deploy.Transient(err), "%v", err)
	})

	t.Run("revoked", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.fake.RevokeRefreshToken()

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.False(t, deploy.Transient(err), "%v", err)
	})
}
