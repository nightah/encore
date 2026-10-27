package api

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/basepath"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/skillpkg"
	"github.com/nightah/encore/internal/store"
)

// Only a stored "kodi" overrides the locales' names.
func invocationChoice(stored string) string {
	if stored == invocationKodi {
		return invocationKodi
	}

	return invocationEncore
}

func loadSources() (*skillmodel.Sources, error) {
	return skillmodel.Load(i18n.FS)
}

func (p Privacy) answered() bool {
	return p.UsesPersonalInfo != nil && p.IsChildDirected != nil && p.IsExportCompliant != nil && p.ContainsAds != nil && p.AllowsPurchases != nil
}

func (p Privacy) unanswered() []string {
	var out []string

	for name, v := range map[string]*bool{
		"uses_personal_info": p.UsesPersonalInfo, "is_child_directed": p.IsChildDirected, "is_export_compliant": p.IsExportCompliant,
		"contains_ads": p.ContainsAds, "allows_purchases": p.AllowsPurchases,
	} {
		if v == nil {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

func (p Privacy) toPkg() skillpkg.Privacy {
	return skillpkg.Privacy{
		AllowsPurchases: p.AllowsPurchases, UsesPersonalInfo: p.UsesPersonalInfo, IsChildDirected: p.IsChildDirected,
		IsExportCompliant: p.IsExportCompliant, ContainsAds: p.ContainsAds,
	}
}

// simulating reports whether the deploy smoke test is running or has just finished.
func (s *skillService) simulating() bool {
	return s.a.now().UnixNano() < s.simulatingUntil.Load()
}

func newSkillService(a *API) *skillService {
	s := &skillService{a: a, pending: map[string]lwaPending{}, deploys: map[string]*deployRecord{}}

	id, _ := store.Setting(context.Background(), a.app.DB().Settings(), skillIDKey, "")
	s.skillID.Store(&id)

	return s
}

// storedSkillID is the skill ID the last deploy recorded; cheap enough for every Alexa request.
func (s *skillService) storedSkillID() string {
	if p := s.skillID.Load(); p != nil {
		return *p
	}

	return ""
}

func (s *skillService) settings(ctx context.Context) (skillSettings, error) {
	return store.Setting(ctx, s.a.app.DB().Settings(), skillSettingsKey, skillSettings{})
}

func (s *skillService) lwaSettings(ctx context.Context) (lwaSettings, error) {
	return store.Setting(ctx, s.a.app.DB().Settings(), lwaSettingsKey, lwaSettings{})
}

func (s *skillService) deployState(ctx context.Context) (deploy.State, error) {
	return store.Setting(ctx, s.a.app.DB().Settings(), deployStateKey, deploy.State{})
}

func (s *skillService) tokens() secretTokenStore { return secretTokenStore{sec: s.a.app.Secrets()} }

func (s *skillService) lwaClient(ctx context.Context, redirect string) (*lwa.Client, error) {
	ls, err := s.lwaSettings(ctx)
	if err != nil {
		return nil, err
	}

	secret, err := s.a.app.Secrets().Get(ctx, lwaSecretRef)

	switch {
	case errors.Is(err, secrets.ErrNotFound):
		return nil, errNoLWACredentials
	case err != nil:
		return nil, err
	}
	defer clear(secret)

	if ls.ClientID == "" {
		return nil, errNoLWACredentials
	}

	return lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: ls.ClientID, ClientSecret: string(secret)},
		RedirectURL: redirect,
		Endpoint:    s.a.opts.LWAEndpoint,
		HTTPClient:  s.a.opts.HTTPClient,
	})
}

// content is alexa.content, in the order the groups are offered.
func (s *skillService) content(ctx context.Context) ([]string, error) {
	eff, err := s.a.prefs.Effective(ctx)
	if err != nil {
		return nil, err
	}

	return orderContent(eff.Alexa.Content), nil
}

// orderContent puts content groups in the order they are offered, dropping duplicates and unknown groups.
func orderContent(in []string) []string {
	out := []string{}

	for _, c := range media.Contents {
		if slices.Contains(in, string(c)) {
			out = append(out, string(c))
		}
	}

	return out
}

func contentGroups(in []string) []media.Content {
	out := make([]media.Content, 0, len(in))
	for _, c := range in {
		out = append(out, media.Content(c))
	}

	return out
}

// locales returns the enabled locales: alexa.locales, or every shipped locale.
func (s *skillService) locales(ctx context.Context) ([]string, error) {
	eff, err := s.a.prefs.Effective(ctx)
	if err != nil {
		return nil, err
	}

	if len(eff.Alexa.Locales) > 0 {
		return slices.Clone(eff.Alexa.Locales), nil
	}

	return s.a.opts.Sources.LocaleNames(), nil
}

// endpointURL is the configured endpoint, or one derived from how the browser reached Encore.
func (s *skillService) endpointURL(ctx context.Context, ss skillSettings) string {
	if ss.Endpoint.URL != "" {
		return ss.Endpoint.URL
	}

	if r := info(ctx).r; r != nil {
		return s.a.auth.ExternalBase(r) + alexaPath
	}

	return ""
}

// packageOptions builds the package options from the saved settings. With placeholderPrivacy, unanswered privacy questions are filled in so the model can be sized before the user has answered them; such a package is never deployed.
func (s *skillService) packageOptions(ctx context.Context, placeholderPrivacy bool) (skillpkg.Options, skillSettings, error) {
	ss, err := s.settings(ctx)
	if err != nil {
		return skillpkg.Options{}, ss, err
	}

	locales, err := s.locales(ctx)
	if err != nil {
		return skillpkg.Options{}, ss, err
	}

	content, err := s.content(ctx)
	if err != nil {
		return skillpkg.Options{}, ss, err
	}

	opts := skillpkg.Options{
		Locales:    locales,
		Content:    contentGroups(content),
		Invocation: ss.Invocation,
		SkillName:  ss.SkillName,
		Endpoint:   skillpkg.Endpoint{URI: s.endpointURL(ctx, ss)},
		Privacy:    ss.Privacy.toPkg(),
	}

	if placeholderPrivacy && !ss.Privacy.answered() {
		no := false
		opts.Privacy = skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &no, ContainsAds: &no}
	}

	if placeholderPrivacy && !strings.HasPrefix(opts.Endpoint.URI, "https://") {
		opts.Endpoint.URI = "https://encore.invalid" + alexaPath
	}

	if s.a.opts.SlotValues != nil {
		if opts.SlotValues, err = s.a.opts.SlotValues(ctx); err != nil {
			return opts, ss, fmt.Errorf("collecting library titles for the skill model: %w", err)
		}
	}

	return opts, ss, nil
}

// settingsHash is the hash of the package opts builds without library titles, or "" if it doesn't build.
func (s *skillService) settingsHash(opts skillpkg.Options) string {
	opts.SlotValues = nil

	pkg, err := skillpkg.Build(s.a.opts.Sources, opts)
	if err != nil {
		return ""
	}

	return pkg.Hash()
}

// updatePending reports whether what would be deployed now differs from the last deploy, and why: "settings" when the skill's settings changed, "library" when only the library titles in the model did, "none" otherwise.
func (s *skillService) updatePending(ctx context.Context, state deploy.State) (bool, string) {
	opts, _, err := s.packageOptions(ctx, false)
	if err != nil {
		return true, "settings"
	}

	pkg, err := skillpkg.Build(s.a.opts.Sources, opts)
	if err != nil {
		return true, "settings"
	}

	switch {
	case !state.NeedsDeploy(pkg.Hash()):
		return false, "none"
	case state.SettingsHash != "" && state.SettingsHash == s.settingsHash(opts):
		return true, "library"
	default:
		return true, "settings"
	}
}

func (s *skillService) inSkillModel(ctx context.Context) func(instanceID int64, it media.Item) bool {
	none := func(int64, media.Item) bool { return false }

	if s.a.opts.SlotValues == nil {
		return none
	}

	values, err := s.a.opts.SlotValues(ctx)
	if err != nil {
		return none
	}

	ids := map[string]bool{}

	for _, vs := range values {
		for _, v := range vs {
			ids[v.ID] = true
		}
	}

	return func(_ int64, it media.Item) bool {
		return ids[fmt.Sprintf("%s/%d", it.Kind, it.ID)]
	}
}

func (s *skillService) info(ctx context.Context) (SkillInfo, error) {
	ss, err := s.settings(ctx)
	if err != nil {
		return SkillInfo{}, err
	}

	ls, err := s.lwaSettings(ctx)
	if err != nil {
		return SkillInfo{}, err
	}

	state, err := s.deployState(ctx)
	if err != nil {
		return SkillInfo{}, err
	}

	locales, err := s.locales(ctx)
	if err != nil {
		return SkillInfo{}, err
	}

	content, err := s.content(ctx)
	if err != nil {
		return SkillInfo{}, err
	}

	tok, _ := s.tokens().Load(ctx)
	reauth, _ := store.Setting(ctx, s.a.app.DB().Settings(), reauthKey, false)
	_, secretErr := s.a.app.Secrets().Get(ctx, lwaSecretRef)

	out := SkillInfo{
		Amazon: AmazonConnection{
			Connected: tok != nil && tok.RefreshToken != "" && !reauth, VendorID: state.VendorID, ProfileClientID: ls.ClientID,
			HasClientSecret: secretErr == nil, NeedsReauthorise: reauth,
		},
		SkillID:          s.storedSkillID(),
		Invocation:       invocationChoice(ss.Invocation),
		InvocationName:   ss.Invocation,
		SkillName:        ss.SkillName,
		LocalesAvailable: s.a.opts.Sources.LocaleNames(),
		LocalesEnabled:   locales,
		Content:          content,
		Endpoint:         SkillEndpointInfo{URL: s.endpointURL(ctx, ss)},
		Privacy:          ss.Privacy,
	}

	if out.InvocationName == "" || out.SkillName == "" {
		if loc, ok := s.a.opts.Sources.Locales[firstOr(locales, "en-US")]; ok {
			if out.SkillName == "" {
				out.SkillName = loc.SkillName
			}
		}
	}

	last := s.lastDeploy(ctx)
	if last != nil {
		out.LastDeploy = Some(last.summary())
	}

	out.State = s.stateOf(state, last)

	if opts, _, err := s.packageOptions(ctx, true); err == nil {
		if pkg, err := skillpkg.Build(s.a.opts.Sources, opts); err == nil {
			if out.InvocationName == "" {
				out.InvocationName = pkg.Invocation(firstOr(pkg.Locales(), ""))
			}

			out.ModelSize = Some(s.modelSize(ctx, pkg, opts))
		}
	}

	out.NeedsDeploy, out.UpdateReason = s.updatePending(ctx, state)

	return out, nil
}

func (s *skillService) stateOf(state deploy.State, last *deployRecord) string {
	s.mu.Lock()
	running := s.running != ""
	s.mu.Unlock()

	switch {
	case running:
		return "deploying"
	case last != nil && last.State == "failed":
		return "failed"
	case state.Hash != "" || state.SkillID != "" && last != nil && last.State == "succeeded":
		return "live"
	default:
		return "not_deployed"
	}
}

func firstOr(s []string, def string) string {
	if len(s) == 0 {
		return def
	}

	return s[0]
}

func (s *skillService) modelSize(ctx context.Context, pkg *skillpkg.Package, opts skillpkg.Options) ModelSize {
	ms := ModelSize{LimitBytes: skillpkg.MaxModelBytes}

	for _, f := range pkg.Files {
		loc, ok := strings.CutPrefix(f.Name, skillpkg.ModelDir)
		if !ok {
			continue
		}

		if len(f.Data) > ms.Bytes {
			ms.Locale, ms.Bytes = strings.TrimSuffix(loc, ".json"), len(f.Data)
		}
	}

	for _, vs := range opts.SlotValues {
		ms.TitlesInModel += len(vs)
	}

	total := 0

	if libs, err := s.a.libraries.List(ctx); err == nil {
		for _, l := range libs {
			total += l.Counts.Movies + l.Counts.Shows
		}
	}

	ms.TitlesServerOnly = max(0, total-ms.TitlesInModel)

	return ms
}

func (a *API) registerSkill(api huma.API) {
	s := a.skill

	huma.Register(api, protected(huma.Operation{OperationID: "getSkill", Method: http.MethodGet, Path: Prefix + "/skill", Tags: []string{"skill"}, Summary: "Get the Alexa skill's settings and deploy state"}),
		func(ctx context.Context, _ *struct{}) (*body[SkillInfo], error) {
			out, err := s.info(ctx)

			return &body[SkillInfo]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateSkillSettings", Method: http.MethodPut, Path: Prefix + "/skill/settings", Tags: []string{"skill"}, Summary: "Change the skill's settings"}),
		func(ctx context.Context, in *struct{ Body SkillSettingsInput }) (*body[SkillInfo], error) {
			if err := s.saveSettings(ctx, in.Body); err != nil {
				return nil, a.settingsProblem(ctx, err)
			}

			out, err := s.info(ctx)

			return &body[SkillInfo]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateSkillContent", Method: http.MethodPut, Path: Prefix + "/skill/content", Tags: []string{"skill"}, Summary: "Change what the skill plays", Description: "Saves alexa.content alone, as the setup wizard does before the first Kodi is added, so its first library sync fetches only what the skill plays. Applies on the next deploy for the skill model, at once for library syncs."}),
		func(ctx context.Context, in *struct {
			Body struct {
				Content []string `json:"content" minItems:"1" enum:"movies,tv,music" doc:"What the skill can play; music includes music videos"`
			}
		},
		) (*body[SkillInfo], error) {
			content := orderContent(in.Body.Content)
			if len(content) == 0 {
				return nil, a.settingsProblem(ctx, fieldError("body.content", "choose at least one of movies, tv or music", nil))
			}

			if err := s.saveContent(ctx, content); err != nil {
				return nil, a.settingsProblem(ctx, err)
			}

			out, err := s.info(ctx)

			return &body[SkillInfo]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "setAmazonCredentials", Method: http.MethodPut, Path: Prefix + "/skill/amazon/credentials", Tags: []string{"skill"}, Summary: "Save the Login with Amazon security profile's client ID and secret", Description: "Changing the client ID forgets the stored authorisation.", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				ClientID     string `json:"client_id" minLength:"1" maxLength:"256"`
				ClientSecret string `json:"client_secret,omitempty" maxLength:"256" doc:"Write-only; omit to keep the stored one"`
			}
		},
		) (*struct{}, error) {
			return nil, a.problemFor(ctx, s.saveCredentials(ctx, strings.TrimSpace(in.Body.ClientID), strings.TrimSpace(in.Body.ClientSecret)))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "authorizeAmazon", Method: http.MethodGet, Path: Prefix + "/skill/amazon/authorize", Tags: []string{"skill"}, Summary: "Get the Login with Amazon consent URL", Description: "return_urls must be listed under the security profile's Allowed Return URLs. Over HTTPS the consent page returns to Encore's callback; otherwise to the local address, whose URL the user pastes back. Until the client ID and secret are saved, url is empty and only return_urls is filled in, so they can be shown while the security profile is being created."}),
		func(ctx context.Context, in *struct {
			ReturnURL string `query:"return_url" doc:"One of return_urls, to choose the flow explicitly"`
			Then      string `query:"then" enum:"alexa-skill,setup" default:"alexa-skill" doc:"The page the callback returns to"`
		},
		) (*body[AuthorizeInfo], error) {
			out, err := s.authorize(ctx, in.ReturnURL, in.Then)

			return &body[AuthorizeInfo]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, public(huma.Operation{OperationID: "lwaCallback", Method: http.MethodGet, Path: lwaCallbackPath, Tags: []string{"skill"}, Summary: "Finish Login with Amazon", Description: "Public; checked against the state of a pending authorisation. Redirects to the page that started it (the Alexa skill page or the setup wizard).", DefaultStatus: http.StatusFound}),
		func(ctx context.Context, in *struct {
			Code             string `query:"code"`
			State            string `query:"state"`
			Error            string `query:"error"`
			ErrorDescription string `query:"error_description"`
		},
		) (*redirect, error) {
			then, err := s.finish(ctx, in.Code, in.State, in.Error, in.ErrorDescription)
			page := basepath.From(ctx) + then

			if err != nil {
				a.log.Warn("Login with Amazon failed", "error", err)

				return &redirect{Status: http.StatusFound, Location: page + "?amazon_error=" + url.QueryEscape(err.Error())}, nil
			}

			return &redirect{Status: http.StatusFound, Location: page + "?amazon=connected"}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "pasteAmazonAuthorization", Method: http.MethodPost, Path: Prefix + "/skill/amazon/paste", Tags: []string{"skill"}, Summary: "Finish Login with Amazon by pasting", Description: "Either the address the browser was sent to after consenting (redirect_url), or a refresh token from `ask util generate-lwa-tokens` (refresh_token).", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			Body struct {
				RedirectURL  string `json:"redirect_url,omitempty"`
				RefreshToken string `json:"refresh_token,omitempty"`
			}
		},
		) (*struct{}, error) {
			var err error

			switch {
			case in.Body.RedirectURL != "":
				code, state, perr := lwa.ParseRedirectURL(in.Body.RedirectURL)
				if perr != nil {
					return nil, fieldError("body.redirect_url", perr.Error(), nil)
				}

				_, err = s.finish(ctx, code, state, "", "")
			case in.Body.RefreshToken != "":
				err = s.pasteRefreshToken(ctx, in.Body.RefreshToken)
			default:
				return nil, fieldError("body", "send redirect_url or refresh_token", nil)
			}

			return nil, a.lwaProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "disconnectAmazon", Method: http.MethodDelete, Path: Prefix + "/skill/amazon", Tags: []string{"skill"}, Summary: "Forget the Login with Amazon authorisation", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := s.tokens().forget(ctx); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			return nil, a.problemFor(ctx, a.app.DB().Settings().Delete(ctx, reauthKey))
		})

	a.registerDeploy(api)
}

func (s *skillService) saveSettings(ctx context.Context, in SkillSettingsInput) error {
	available := s.a.opts.Sources.LocaleNames()

	locales := []string{}

	for _, l := range in.LocalesEnabled {
		if !slices.Contains(available, l) {
			return fieldError("body.locales_enabled", "is not a locale Encore ships", l)
		}

		if !slices.Contains(locales, l) {
			locales = append(locales, l)
		}
	}

	if len(locales) == 0 {
		return fieldError("body.locales_enabled", "choose at least one locale", nil)
	}

	content := orderContent(in.Content)
	if in.Content != nil && len(content) == 0 {
		return fieldError("body.content", "choose at least one of movies, tv or music", nil)
	}

	ep := in.Endpoint
	if ep.URL != "" {
		u, err := url.Parse(ep.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fieldError("body.endpoint.url", "must be an https:// URL; Alexa only calls HTTPS endpoints", ep.URL)
		}
	}

	// The stored override is empty for Encore, so each locale keeps its own name.
	invocation := ""

	switch in.Invocation {
	case "", invocationEncore:
	case invocationKodi:
		invocation = invocationKodi
	default:
		return fieldError("body.invocation", "must be encore or kodi", in.Invocation)
	}

	next := skillSettings{Invocation: invocation, SkillName: strings.TrimSpace(in.SkillName), Endpoint: ep, Privacy: in.Privacy}

	// A trial build catches an invalid invocation name or skill name before it is saved.
	trial := skillpkg.Options{
		Locales: locales, Content: contentGroups(content), Invocation: next.Invocation, SkillName: next.SkillName,
		Endpoint: skillpkg.Endpoint{URI: "https://encore.invalid" + alexaPath},
		Privacy:  Privacy{UsesPersonalInfo: new(false), IsChildDirected: new(false), IsExportCompliant: new(false), ContainsAds: new(false), AllowsPurchases: new(false)}.toPkg(),
	}

	if _, err := skillpkg.Build(s.a.opts.Sources, trial); err != nil {
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "skillpkg: "))
	}

	eff, err := s.a.prefs.Effective(ctx)
	if err != nil {
		return err
	}

	// Choosing every locale while alexa.locales is unset is no change.
	allByDefault := len(eff.Alexa.Locales) == 0 && slices.Equal(available, sortedCopy(locales))
	if !allByDefault && !slices.Equal(eff.Alexa.Locales, locales) {
		if _, err := s.a.prefs.Apply(ctx, []Change{{Key: "alexa.locales", Value: locales}}); err != nil {
			return err
		}
	}

	if in.Content != nil {
		if err := s.saveContent(ctx, content); err != nil {
			return err
		}
	}

	return s.a.app.DB().Settings().Set(ctx, skillSettingsKey, next)
}

// saveContent stores what the skill plays (alexa.content), given in the order offered, and brings the libraries in line: they stop fetching what the skill no longer plays and fetch what it now does.
func (s *skillService) saveContent(ctx context.Context, content []string) error {
	eff, err := s.a.prefs.Effective(ctx)
	if err != nil {
		return err
	}

	if slices.Equal(orderContent(eff.Alexa.Content), content) {
		return nil
	}

	if _, err := s.a.prefs.Apply(ctx, []Change{{Key: "alexa.content", Value: content}}); err != nil {
		return err
	}

	s.a.app.ApplyContent(ctx)

	return nil
}

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)

	return c
}

func (s *skillService) saveCredentials(ctx context.Context, clientID, secret string) error {
	ls, err := s.lwaSettings(ctx)
	if err != nil {
		return err
	}

	if secret == "" {
		if _, err := s.a.app.Secrets().Get(ctx, lwaSecretRef); err != nil {
			return fieldError("body.client_secret", "is required the first time", nil)
		}
	} else if err := s.a.app.Secrets().Put(ctx, lwaSecretRef, []byte(secret)); err != nil {
		return err
	}

	if ls.ClientID != "" && ls.ClientID != clientID {
		// A token belongs to the client that obtained it.
		if err := s.tokens().forget(ctx); err != nil {
			return err
		}
	}

	return s.a.app.DB().Settings().Set(ctx, lwaSettingsKey, lwaSettings{ClientID: clientID})
}

func (s *skillService) authorize(ctx context.Context, returnURL, then string) (AuthorizeInfo, error) {
	r := info(ctx).r
	callback := s.a.auth.ExternalBase(r) + lwaCallbackPath
	urls := []string{callback, localReturnURL}

	redirect := localReturnURL
	if strings.HasPrefix(callback, "https://") {
		redirect = callback
	}

	if returnURL != "" {
		if !slices.Contains(urls, returnURL) {
			return AuthorizeInfo{}, fieldError("query.return_url", "must be one of return_urls", returnURL)
		}

		redirect = returnURL
	}

	client, err := s.lwaClient(ctx, redirect)
	if errors.Is(err, errNoLWACredentials) {
		return AuthorizeInfo{ReturnURLs: urls}, nil
	}

	if err != nil {
		return AuthorizeInfo{}, err
	}

	state, pkce := lwa.NewState(), lwa.NewPKCE()
	now := s.a.now()

	s.mu.Lock()
	for k, p := range s.pending {
		if now.After(p.expires) {
			delete(s.pending, k)
		}
	}

	s.pending[state] = lwaPending{verifier: pkce.Verifier, redirect: redirect, page: then, expires: now.Add(lwaPendingTTL)}
	s.mu.Unlock()

	return AuthorizeInfo{URL: client.AuthURL(state, pkce), ReturnURLs: urls}, nil
}

// finish redeems an authorisation code for a pending state and stores the token. It returns the page to go back to: the one that started the authorisation, or the Alexa skill page for a state it did not issue.
func (s *skillService) finish(ctx context.Context, code, state, lwaError, lwaDescription string) (string, error) {
	s.mu.Lock()
	p, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()

	if !ok || s.a.now().After(p.expires) {
		return "alexa-skill", errors.New("this authorisation was not started here or has expired; start again")
	}

	page := cmp.Or(p.page, "alexa-skill")

	if lwaError != "" {
		return page, errors.New(strings.TrimSpace(lwaError + " " + lwaDescription))
	}

	client, err := s.lwaClient(ctx, p.redirect)
	if err != nil {
		return page, err
	}

	tok, err := client.Exchange(ctx, code, p.verifier)
	if err != nil {
		return page, err
	}

	return page, s.storeToken(ctx, tok)
}

func (s *skillService) pasteRefreshToken(ctx context.Context, refresh string) error {
	client, err := s.lwaClient(ctx, "")
	if err != nil {
		return err
	}

	tok, err := client.FromRefreshToken(ctx, refresh)
	if err != nil {
		return err
	}

	return s.storeToken(ctx, tok)
}

func (s *skillService) storeToken(ctx context.Context, tok *oauth2.Token) error {
	if err := s.tokens().Save(ctx, tok); err != nil {
		return err
	}

	s.a.log.Info("connected to Amazon with Login with Amazon")

	return s.a.app.DB().Settings().Delete(ctx, reauthKey)
}

func (a *API) lwaProblem(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, lwa.ErrInvalidClient):
		return huma.Error422UnprocessableEntity("Amazon rejected the client ID or secret; check the security profile")
	case errors.Is(err, lwa.ErrReauthorise):
		return huma.Error422UnprocessableEntity("Amazon rejected the authorisation; start again")
	}

	var se huma.StatusError
	if errors.As(err, &se) {
		return err
	}

	if strings.HasPrefix(err.Error(), "lwa:") || strings.Contains(err.Error(), "authorisation") {
		return huma.Error422UnprocessableEntity(err.Error())
	}

	return a.problemFor(ctx, err)
}

func (s *skillService) exportPackage(ctx context.Context) ([]byte, error) {
	opts, ss, err := s.packageOptions(ctx, false)
	if err != nil {
		return nil, err
	}

	if !ss.Privacy.answered() {
		return nil, fieldError("privacy", "answer the privacy and compliance questions first: "+strings.Join(ss.Privacy.unanswered(), ", "), nil)
	}

	var buf bytes.Buffer

	pkg, err := skillpkg.Build(s.a.opts.Sources, opts)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "skillpkg: "))
	}

	if err := pkg.Export(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
