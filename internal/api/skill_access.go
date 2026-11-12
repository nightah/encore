package api

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/store"
)

// ReadSkillAccess reads what Encore keeps for reaching the skill it deploys through SMAPI: the Login with Amazon security profile and token from the Alexa skill page, the skill's ID and the locale to talk to it in: the default locale from Settings when the skill has it, else the skill's first. It only reads, so a tool can use it beside a running server. ErrNoSkillAccess means Encore isn't connected to Amazon, or hasn't deployed its skill.
func ReadSkillAccess(ctx context.Context, cfg *config.Config, db *store.DB, sec *secrets.Store) (SkillAccess, error) {
	settings := db.Settings()

	eff, err := NewPrefs(cfg, db, sec).Effective(ctx)
	if err != nil {
		return SkillAccess{}, err
	}

	ls, err := store.Setting(ctx, settings, lwaSettingsKey, lwaSettings{})
	if err != nil {
		return SkillAccess{}, err
	}

	state, err := store.Setting(ctx, settings, deployStateKey, deploy.State{})
	if err != nil {
		return SkillAccess{}, err
	}

	id, err := store.Setting(ctx, settings, skillIDKey, "")
	if err != nil {
		return SkillAccess{}, err
	}

	loc, err := store.Setting(ctx, settings, localeSetting, LocaleSettings{})
	if err != nil {
		return SkillAccess{}, err
	}

	// As the server picks it: the default locale when the skill has it, else the skill's first.
	if enabled := eff.Alexa.Locales; len(enabled) > 0 && !slices.Contains(enabled, loc.DefaultLocale) {
		loc.DefaultLocale = enabled[0]
	}

	access := SkillAccess{ClientID: ls.ClientID, SkillID: cmp.Or(state.SkillID, id), Locale: loc.DefaultLocale}

	switch {
	case access.SkillID == "":
		return SkillAccess{}, errors.Join(ErrNoSkillAccess, errors.New("no skill has been deployed"))
	case access.ClientID == "":
		return SkillAccess{}, errors.Join(ErrNoSkillAccess, errors.New("no security profile is set on the Alexa skill page"))
	}

	secret, err := sec.Get(ctx, lwaSecretRef)

	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return SkillAccess{}, errors.Join(ErrNoSkillAccess, errors.New("the security profile has no client secret"))
	case err != nil:
		return SkillAccess{}, err
	}

	access.ClientSecret = string(secret)
	clear(secret)

	tok, err := secretTokenStore{sec: sec}.Load(ctx)

	switch {
	case err != nil:
		return SkillAccess{}, err
	case tok == nil || tok.RefreshToken == "":
		return SkillAccess{}, errors.Join(ErrNoSkillAccess, errors.New("not connected to Amazon on the Alexa skill page"))
	}

	access.RefreshToken = tok.RefreshToken

	return access, nil
}
