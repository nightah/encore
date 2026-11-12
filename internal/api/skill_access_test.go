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
	for name, tc := range map[string]struct {
		locales  []string
		def, out string
	}{
		"the default":                   {locales: []string{"en-GB", "en-AU"}, def: "en-AU", out: "en-AU"},
		"the skill's first":             {locales: []string{"en-AU", "en-GB"}, def: "de-DE", out: "en-AU"},
		"the default with every locale": {def: "en-GB", out: "en-GB"},
		"the first shipped locale":      {out: "ar-SA"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)

			settings := h.app.DB().Settings()
			require.NoError(t, settings.Set(t.Context(), localeSetting, LocaleSettings{Timezone: "Australia/Melbourne", DefaultLocale: tc.def}))

			if tc.locales != nil {
				require.NoError(t, settings.Set(t.Context(), overridesSetting, map[string]any{"alexa.locales": tc.locales}))
			}

			got, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
			require.NoError(t, err)
			assert.Equal(t, tc.out, got.Locale)
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
	for name, tc := range map[string]struct {
		undo func(t *testing.T, h *harness)
		want string
	}{
		"no skill": {func(t *testing.T, h *harness) {
			require.NoError(t, h.app.DB().Settings().Delete(t.Context(), deployStateKey))
		}, "no skill has been deployed"},
		"no client ID": {func(t *testing.T, h *harness) {
			require.NoError(t, h.app.DB().Settings().Delete(t.Context(), lwaSettingsKey))
		}, "no security profile"},
		"no secret": {func(t *testing.T, h *harness) { require.NoError(t, h.app.Secrets().Delete(t.Context(), lwaSecretRef)) }, "no client secret"},
		"no token": {func(t *testing.T, h *harness) {
			require.NoError(t, secretTokenStore{sec: h.app.Secrets()}.forget(t.Context()))
		}, "not connected to Amazon"},
		"no refresh token": {func(t *testing.T, h *harness) {
			require.NoError(t, secretTokenStore{sec: h.app.Secrets()}.Save(t.Context(), &oauth2.Token{AccessToken: "Atza|access"}))
		}, "not connected to Amazon"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)
			tc.undo(t, h)

			_, err := ReadSkillAccess(t.Context(), h.cfg, h.app.DB(), h.app.Secrets())
			require.ErrorIs(t, err, ErrNoSkillAccess)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestReadSkillAccessReportsUnreadableSettings(t *testing.T) {
	for _, key := range []string{overridesSetting, lwaSettingsKey, deployStateKey, skillIDKey, localeSetting} {
		t.Run(key, func(t *testing.T) {
			h := newHarness(t, harnessOpts{noKodi: true})
			connectSkill(t, h)
			require.NoError(t, h.app.DB().Settings().Set(t.Context(), key, []int{1}))

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
