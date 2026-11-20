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

	testCases := []struct {
		name     string
		have     error
		expected bool
	}{
		{name: "ShouldNotRetryNone"},
		{name: "ShouldNotRetryReauthorise", have: fmt.Errorf("smapi: getting access token: %w", lwa.ErrReauthorise)},
		{name: "ShouldNotRetryClientRejected", have: lwa.ErrInvalidClient},
		{name: "ShouldNotRetryNeverAuthorised", have: lwa.ErrNotAuthorised},
		{name: "ShouldRetryPollTimeout", have: fmt.Errorf("%w: %w", smapi.ErrPollTimeout, context.DeadlineExceeded), expected: true},
		{name: "ShouldRetryInterrupted", have: context.Canceled, expected: true},
		{name: "ShouldRetryBuildAlreadyRunning", have: &smapi.Error{Op: "import skill package", StatusCode: http.StatusConflict}, expected: true},
		{name: "ShouldRetryThrottled", have: &smapi.Error{Op: "list vendors", StatusCode: http.StatusTooManyRequests}, expected: true},
		{name: "ShouldRetryUnavailable", have: fmt.Errorf("wrapped: %w", &smapi.Error{Op: "list vendors", StatusCode: http.StatusServiceUnavailable}), expected: true},
		{name: "ShouldNotRetryValidation", have: &smapi.Error{Op: "import skill package", StatusCode: http.StatusBadRequest}},
		{name: "ShouldNotRetryUnauthorisedAfterRefresh", have: &smapi.Error{Op: "list vendors", StatusCode: http.StatusUnauthorized}},
		{name: "ShouldRetryNetwork", have: fmt.Errorf("smapi: list vendors: %w", network), expected: true},
		{name: "ShouldRetryBuildWithoutDetails", have: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": unexplained, "en-GB": unreported}}, expected: true},
		{name: "ShouldNotRetryNamedBuildError", have: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": unexplained, "en-GB": named}}},
		{name: "ShouldNotRetryCodedUnexplainedMessage", have: &deploy.Error{Step: deploy.StepBuild, Locales: map[string][]smapi.Problem{"en-US": {{Code: "X", Message: "build failed without details"}}}}},
		{name: "ShouldNotRetryImportErrors", have: &deploy.Error{Step: deploy.StepImport, Problems: named}},
		{name: "ShouldRetryImportFailedSilently", have: &deploy.Error{Step: deploy.StepImport}, expected: true},
		{name: "ShouldNotRetryBuildFailedSilently", have: &deploy.Error{Step: deploy.StepBuild}},
		{name: "ShouldNotRetryVendorChoice", have: &deploy.VendorChoiceError{}},
		{name: "ShouldNotRetryPackageTooLarge", have: errors.New("skillpkg: locale en-US: model is 1600000 bytes, over Amazon's limit")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, deploy.Transient(tc.have))
		})
	}
}

func TestTransientDeployFailures(t *testing.T) {
	t.Run("ShouldRetryBuildWithoutDetails", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{ImportWithoutModels: true})
		h.override("GET /v1/skills/{id}/status", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, `{"interactionModel":{"en-US":{"lastUpdateRequest":{"status":"FAILED"}},"en-GB":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}}`)
		})

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.True(t, deploy.Transient(err), "%v", err)
	})

	t.Run("ShouldRetryBuildAlreadyRunning", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.override("POST /v1/skills/imports", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusConflict, `{"message":"Another build is in progress"}`)
		})

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.True(t, deploy.Transient(err), "%v", err)
	})

	t.Run("ShouldNotRetryNamedBuildError", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.fake.FailLocale("en-GB", "Sample utterance conflicts with another intent")
		h.fake.FailLocale("en-US", "Sample utterance conflicts with another intent")

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.ErrorIs(t, err, deploy.ErrBuildFailed)
		assert.False(t, deploy.Transient(err), "%v", err)
	})

	t.Run("ShouldNotRetryRevokedRefreshToken", func(t *testing.T) {
		h := newHarness(t, smapitest.Options{})
		h.fake.RevokeRefreshToken()

		_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
		require.Error(t, err)
		assert.False(t, deploy.Transient(err), "%v", err)
	})
}
