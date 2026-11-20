package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/smapi/deploy"
)

// connectSkill stores what the Alexa skill page and a deploy leave behind: a security profile, the token Amazon issued for it and the deployed skill.
func connectSkill(t *testing.T, h *harness) {
	t.Helper()

	ctx := t.Context()
	settings := h.app.DB().Settings()

	require.NoError(t, settings.Set(ctx, lwaSettingsKey, lwaSettings{ClientID: "amzn1.application-oa2-client.encore"}))
	require.NoError(t, h.app.Secrets().Put(ctx, lwaSecretRef, []byte("client-secret")))
	require.NoError(t, secretTokenStore{sec: h.app.Secrets()}.Save(ctx, &oauth2.Token{AccessToken: "Atza|access", RefreshToken: "Atzr|refresh"}))
	require.NoError(t, settings.Set(ctx, deployStateKey, deploy.State{VendorID: "M1", SkillID: "amzn1.ask.skill.deployed"}))
}

func TestReadSkillAccess(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	connectSkill(t, h)

	got, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
	require.NoError(t, err)
	assert.Equal(t, SkillAccess{
		ClientID:     "amzn1.application-oa2-client.encore",
		ClientSecret: "client-secret",
		RefreshToken: "Atzr|refresh",
		SkillID:      "amzn1.ask.skill.deployed",
		Locale:       "ar-SA",
	}, got, "with no locales chosen and no default, the server speaks the first shipped locale")
}

func TestReadSkillAccessLocale(t *testing.T) {
	testCases := []struct {
		name     string
		locales  []string
		def      string
		expected string
	}{
		{name: "ShouldSpeakTheDefault", locales: []string{"en-GB", "en-AU"}, def: "en-AU", expected: "en-AU"},
		{name: "ShouldSpeakTheSkillsFirstLocale", locales: []string{"en-AU", "en-GB"}, def: "de-DE", expected: "en-AU"},
		{name: "ShouldSpeakTheDefaultWithEveryLocale", def: "en-GB", expected: "en-GB"},
		{name: "ShouldSpeakTheFirstShippedLocale", expected: "ar-SA"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)

			settings := h.app.DB().Settings()
			require.NoError(t, settings.Set(t.Context(), localeSetting, LocaleSettings{Timezone: "Australia/Melbourne", DefaultLocale: tc.def}))

			if tc.locales != nil {
				require.NoError(t, settings.Set(t.Context(), overridesSetting, map[string]any{"alexa.locales": tc.locales}))
			}

			got, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got.Locale)
		})
	}
}

func TestReadSkillAccessSkillIDFromItsSetting(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	connectSkill(t, h)
	require.NoError(t, h.app.DB().Settings().Set(t.Context(), deployStateKey, deploy.State{}))
	require.NoError(t, h.app.DB().Settings().Set(t.Context(), skillIDKey, "amzn1.ask.skill.adopted"))

	got, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
	require.NoError(t, err)
	assert.Equal(t, "amzn1.ask.skill.adopted", got.SkillID, "a skill Encore adopted rather than deployed")
}

func TestReadSkillAccessNeedsAConnectedSkill(t *testing.T) {
	testCases := []struct {
		name string
		undo func(t *testing.T, h *harness)
		err  string
	}{
		{name: "ShouldNeedADeployedSkill", undo: func(t *testing.T, h *harness) {
			require.NoError(t, h.app.DB().Settings().Delete(t.Context(), deployStateKey))
		}, err: "no skill has been deployed"},
		{name: "ShouldNeedAClientID", undo: func(t *testing.T, h *harness) {
			require.NoError(t, h.app.DB().Settings().Delete(t.Context(), lwaSettingsKey))
		}, err: "no security profile"},
		{name: "ShouldNeedAClientSecret", undo: func(t *testing.T, h *harness) { require.NoError(t, h.app.Secrets().Delete(t.Context(), lwaSecretRef)) }, err: "no client secret"},
		{name: "ShouldNeedAToken", undo: func(t *testing.T, h *harness) {
			require.NoError(t, secretTokenStore{sec: h.app.Secrets()}.forget(t.Context()))
		}, err: "not connected to Amazon"},
		{name: "ShouldNeedARefreshToken", undo: func(t *testing.T, h *harness) {
			require.NoError(t, secretTokenStore{sec: h.app.Secrets()}.Save(t.Context(), &oauth2.Token{AccessToken: "Atza|access"}))
		}, err: "not connected to Amazon"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)
			tc.undo(t, h)

			_, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
			require.ErrorIs(t, err, ErrNoSkillAccess)
			assert.ErrorContains(t, err, tc.err)
		})
	}
}

func TestReadSkillAccessReportsUnreadableSettings(t *testing.T) {
	testCases := []struct {
		name string
		key  string
	}{
		{name: "ShouldReportUnreadableOverrides", key: overridesSetting},
		{name: "ShouldReportUnreadableLWASettings", key: lwaSettingsKey},
		{name: "ShouldReportAnUnreadableDeployState", key: deployStateKey},
		{name: "ShouldReportAnUnreadableSkillID", key: skillIDKey},
		{name: "ShouldReportUnreadableLocaleSettings", key: localeSetting},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)
			require.NoError(t, h.app.DB().Settings().Set(t.Context(), tc.key, []int{1}))

			_, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrNoSkillAccess)
		})
	}

	h := newHarness(t, harnessOpts{noKodi: true})
	connectSkill(t, h)
	require.NoError(t, h.app.Secrets().Put(t.Context(), lwaTokenRef, []byte("{not json")))

	_, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoSkillAccess, "a damaged token is an error, not a missing one")

	keyless, err := secrets.OpenWithKey(h.app.DB().SecretRows(), nil)
	require.NoError(t, err)
	require.NoError(t, h.app.Secrets().Put(t.Context(), lwaTokenRef, []byte(`{"refresh_token": "Atzr|refresh"}`)))

	_, err = ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), keyless)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoSkillAccess, "secrets it can't decrypt are an error, not missing ones")
}
