package deploy_test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/skillpkg"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

type harness struct {
	fake     *smapitest.Server
	deployer *deploy.Deployer

	mu     sync.Mutex
	events []deploy.Event
}

func newHarness(t *testing.T, opts smapitest.Options) *harness {
	t.Helper()

	if opts.Pending == 0 {
		opts.Pending = 2
	}

	fake := smapitest.New(t, opts)
	o := fake.Options()

	lc, err := lwa.New(lwa.Config{
		Credentials: lwa.Credentials{ClientID: o.ClientID, ClientSecret: o.ClientSecret},
		Endpoint:    fake.Endpoint(),
	})
	require.NoError(t, err)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: o.RefreshToken}))

	client, err := smapi.New(lc.TokenSource(store), smapi.Options{
		BaseURL: fake.URL,
		Retry:   smapi.RetryPolicy{Base: time.Millisecond, Max: 2 * time.Millisecond, MaxRetryAfter: time.Millisecond},
		Poll:    smapi.PollConfig{Interval: time.Millisecond, MaxInterval: 2 * time.Millisecond, Timeout: 10 * time.Second},
	})
	require.NoError(t, err)

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	return &harness{fake: fake, deployer: deploy.New(client, src)}
}

func (h *harness) request(state deploy.State) deploy.Request {
	no, yes := false, true

	return deploy.Request{
		State: state,
		Package: skillpkg.Options{
			Locales:  []string{"en-US", "en-GB"},
			Endpoint: skillpkg.Endpoint{URI: "https://encore.example.com/api/v1/alexa"},
			Privacy:  skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no},
		},
		Simulate: true,
		Progress: func(ev deploy.Event) {
			h.mu.Lock()
			h.events = append(h.events, ev)
			h.mu.Unlock()
		},
	}
}

// timeline lists each step's final state, in first-seen order.
func (h *harness) timeline() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var order []deploy.Step

	last := map[deploy.Step]deploy.StepState{}

	for _, ev := range h.events {
		if _, ok := last[ev.Step]; !ok {
			order = append(order, ev.Step)
		}

		last[ev.Step] = ev.State
	}

	out := make([]string, 0, len(order))
	for _, s := range order {
		out = append(out, string(s)+":"+string(last[s]))
	}

	return out
}

func TestDeployNewSkill(t *testing.T) {
	h := newHarness(t, smapitest.Options{TokenTTL: 5 * time.Second})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)

	assert.False(t, res.Skipped)
	assert.Equal(t, smapitest.DefaultVendorID, res.State.VendorID)
	require.NotEmpty(t, res.State.SkillID)
	assert.Len(t, res.State.Hash, 64)
	assert.Empty(t, res.Warnings)

	assert.Equal(t, []string{
		"package:done", "vendor:done", "skill:done", "upload:done", "import:done",
		"build:done", "enable:done", "simulate:done",
	}, h.timeline())

	for _, loc := range []string{"en-US", "en-GB"} {
		assert.Equal(t, smapi.StatusSucceeded, res.Locales[loc].Status, loc)
	}

	// Without a primary locale, only the first package locale is smoke-tested.
	require.NotNil(t, res.Locales["en-GB"].Simulation)
	assert.True(t, res.Locales["en-GB"].Simulation.Invoked)
	assert.Equal(t, []string{"Welcome to encore."}, res.Locales["en-GB"].Simulation.Captions)
	assert.Nil(t, res.Locales["en-US"].Simulation)

	sk, ok := h.fake.Skill(res.State.SkillID)
	require.True(t, ok)
	assert.True(t, sk.Enabled)
	assert.Equal(t, map[string]string{"en-US": "Encore", "en-GB": "Encore"}, sk.Names)
	assert.Contains(t, sk.Models, "en-GB")

	// Short-lived tokens forced refreshes along the way.
	assert.Greater(t, h.fake.Refreshes(), 5)
}

func TestDeployIsIdempotent(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	first, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)

	uploads := h.fake.Calls("POST /v1/skills/uploads")

	h.events = nil

	second, err := h.deployer.Deploy(t.Context(), h.request(first.State))
	require.NoError(t, err)
	assert.True(t, second.Skipped)
	assert.Equal(t, first.State, second.State)
	assert.Equal(t, uploads, h.fake.Calls("POST /v1/skills/uploads"))
	assert.Equal(t, 1, h.fake.SkillCount())
	assert.Equal(t, "package:done", h.timeline()[0])
	assert.Equal(t, "enable:skipped", h.timeline()[6])

	req := h.request(first.State)
	req.Force = true

	forced, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	assert.False(t, forced.Skipped)
	assert.Equal(t, first.State.SkillID, forced.State.SkillID)

	sk, _ := h.fake.Skill(first.State.SkillID)
	assert.Equal(t, 2, sk.Imports)
	assert.Equal(t, 1, h.fake.SkillCount())
}

func TestDeployFindsSkillWhenStateLost(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-US": "Something else"})
	existing := h.fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-GB": "Encore"})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.Equal(t, existing, res.State.SkillID)
	assert.Equal(t, 2, h.fake.SkillCount())
	assert.Equal(t, 0, h.fake.Calls("POST /v1/skills/imports"))
	assert.Equal(t, 1, h.fake.Calls("POST /v1/skills/{skillId}/imports"))
}

func TestDeployRecreatesDeletedSkill(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{VendorID: smapitest.DefaultVendorID, SkillID: "amzn1.ask.skill.gone"}))
	require.NoError(t, err)
	assert.NotEqual(t, "amzn1.ask.skill.gone", res.State.SkillID)
	assert.NotEmpty(t, res.State.SkillID)
	assert.Contains(t, res.Warnings[0], "no longer exists")
	assert.Equal(t, 0, h.fake.Calls("GET /v1/vendors"), "a stored vendor is reused")
}

func TestDeployLocaleFailure(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	first, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)

	h.fake.FailLocale("en-GB", "Sample utterance \"play\" conflicts with another intent")

	h.events = nil
	req := h.request(first.State)
	req.Force = true

	res, err := h.deployer.Deploy(t.Context(), req)
	require.ErrorIs(t, err, deploy.ErrBuildFailed)

	var de *deploy.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, deploy.StepBuild, de.Step)
	require.Contains(t, de.Locales, "en-GB")
	assert.Contains(t, de.Locales["en-GB"][0].Message, "conflicts with another intent")
	assert.NotContains(t, de.Locales, "en-US")
	assert.Contains(t, err.Error(), "en-GB: INVALID_INTERACTION_MODEL")

	assert.Equal(t, smapi.StatusFailed, res.Locales["en-GB"].Status)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-US"].Status)

	// The skill ID is kept for the retry, but the hash is cleared so the next deploy is not skipped.
	assert.Equal(t, first.State.SkillID, res.State.SkillID)
	assert.Empty(t, res.State.Hash)

	tl := h.timeline()
	assert.Contains(t, tl, "import:failed")
	assert.Contains(t, tl, "build:failed")
	assert.NotContains(t, tl, "enable:done")
	assert.Equal(t, 1, h.fake.Calls("PUT /v1/skills/{skillId}/stages/{stage}/enablement"), "only the first deploy enabled the skill")
}

// The live API keys locales with underscores in places (interactionModels.en_US); builds must still be recognised as finished.
func TestDeployLocaleKeysWithUnderscores(t *testing.T) {
	h := newHarness(t, smapitest.Options{UnderscoreLocales: true})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-GB"].Status)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-US"].Status)
}

// A failed import whose locale never reports a build must fail with the import's errors at once, not wait out the poll timeout.
func TestDeployFailedImportDoesNotWait(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.FailLocale("en-GB", "Parsing error in sample")
	h.fake.UnreportLocale("en-GB")

	start := time.Now()
	_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))

	require.Error(t, err)
	require.NotErrorIs(t, err, smapi.ErrPollTimeout)
	assert.Less(t, time.Since(start), 5*time.Second)

	var de *deploy.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, deploy.StepImport, de.Step)
	assert.Contains(t, err.Error(), "Parsing error in sample", "the failed resource's errors are reported")
	assert.Contains(t, h.timeline(), "import:failed")
}

// The live API: the import reports each locale's model built, while the skill status doesn't list the locale. The import is authoritative.
func TestDeployTrustsTheImportsBuild(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.UnreportLocale("en-GB")
	h.fake.UnreportLocale("en-US")

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-GB"].Status)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-US"].Status)
	assert.Contains(t, h.timeline(), "build:done")
}

// The live API: redeploying an unchanged model reports it SKIPPED in the import, and the unfiltered skill status lists no models. The deploy must still succeed.
func TestDeployUnchangedModelIsSkipped(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	first, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)

	req := h.request(first.State)
	req.Force = true

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-GB"].Status)
	assert.Equal(t, smapi.StatusSucceeded, res.Locales["en-US"].Status)
}

// When the import doesn't report a locale's model and the skill status never does either, the build fails instead of hanging.
func TestDeployLocaleNeverReported(t *testing.T) {
	h := newHarness(t, smapitest.Options{ImportWithoutModels: true})
	h.fake.UnreportLocale("en-GB")

	_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.ErrorIs(t, err, deploy.ErrBuildFailed)
	require.NotErrorIs(t, err, smapi.ErrPollTimeout)

	var de *deploy.Error
	require.ErrorAs(t, err, &de)
	require.Contains(t, de.Locales, "en-GB")
	assert.Contains(t, de.Locales["en-GB"][0].Message, "reported no build")
	assert.NotContains(t, de.Locales, "en-US")
}

func TestDeployRetriesThrottling(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.Inject(http.StatusTooManyRequests, 3)
	h.fake.Inject(http.StatusServiceUnavailable, 1)

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.NotEmpty(t, res.State.Hash)
	assert.Equal(t, 4, h.fake.Injected())
}

func TestDeployRevokedAuthorisation(t *testing.T) {
	h := newHarness(t, smapitest.Options{})
	h.fake.RevokeRefreshToken()

	_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.ErrorIs(t, err, lwa.ErrReauthorise)
	assert.Contains(t, h.timeline(), "vendor:failed")
}

func TestDeployVendorChoice(t *testing.T) {
	h := newHarness(t, smapitest.Options{Vendors: []smapitest.Vendor{{ID: "A", Name: "One"}, {ID: "B", Name: "Two"}}})

	_, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))

	var vc *deploy.VendorChoiceError
	require.ErrorAs(t, err, &vc)
	require.Len(t, vc.Vendors, 2)

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{VendorID: "B"}))
	require.NoError(t, err)
	assert.Equal(t, "B", res.State.VendorID)
}

func TestDeployWithoutSimulation(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	req := h.request(deploy.State{})
	req.Simulate = false

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)
	assert.Nil(t, res.Locales["en-US"].Simulation)
	assert.Contains(t, h.timeline(), "simulate:skipped")
	assert.Equal(t, 0, h.fake.Calls("POST /v2/skills/{skillId}/stages/{stage}/simulations"))
}

func TestDeployPrivacyUnanswered(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	req := h.request(deploy.State{})
	req.Package.Privacy.IsChildDirected = nil

	_, err := h.deployer.Deploy(t.Context(), req)
	require.ErrorIs(t, err, skillpkg.ErrPrivacyUnanswered)
	assert.Equal(t, []string{"package:failed"}, h.timeline())
	assert.Equal(t, 0, h.fake.Calls("GET /v1/vendors"))
}

func TestResync(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	first, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)

	movies := map[string][]skillmodel.SlotValue{"AMAZON.Movie": {{Value: "Heat"}, {Value: "Ronin"}}}

	req := h.request(first.State)
	opts := req.Package
	opts.SlotValues = movies

	pkg, err := h.deployer.Build(opts)
	require.NoError(t, err)
	assert.True(t, first.State.NeedsDeploy(pkg.Hash()))

	second, err := h.deployer.Resync(t.Context(), req, movies)
	require.NoError(t, err)
	assert.False(t, second.Skipped)
	assert.Equal(t, pkg.Hash(), second.State.Hash)
	assert.Equal(t, first.State.SkillID, second.State.SkillID)

	sk, _ := h.fake.Skill(second.State.SkillID)
	assert.Contains(t, string(sk.Models["en-US"]), "Ronin")

	third, err := h.deployer.Resync(t.Context(), h.request(second.State), movies)
	require.NoError(t, err)
	assert.True(t, third.Skipped)
	assert.False(t, second.State.NeedsDeploy(pkg.Hash()))
}

func TestExportPackage(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	var buf bytes.Buffer
	require.NoError(t, h.deployer.ExportPackage(&buf, h.request(deploy.State{}).Package))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	assert.Contains(t, names, "ask-resources.json")
	assert.Contains(t, names, "skill-package/skill.json")
}

func TestOpenPhrase(t *testing.T) {
	assert.Equal(t, "open encore", deploy.OpenPhrase("en-AU", "encore"))
	assert.Equal(t, "öffne encore", deploy.OpenPhrase("de-DE", "encore"))
	assert.Empty(t, deploy.OpenPhrase("pt-BR", "encore"))
}

func TestStateNeedsDeploy(t *testing.T) {
	assert.True(t, deploy.State{}.NeedsDeploy("h"))
	assert.True(t, deploy.State{SkillID: "s", Hash: "a"}.NeedsDeploy("b"))
	assert.False(t, deploy.State{SkillID: "s", Hash: "a"}.NeedsDeploy("a"))
}

func TestDeploySimulatesPrimaryLocale(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	req := h.request(deploy.State{})
	req.PrimaryLocale = "en-US"

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)

	require.NotNil(t, res.Locales["en-US"].Simulation)
	assert.True(t, res.Locales["en-US"].Simulation.Invoked)
	assert.Nil(t, res.Locales["en-GB"].Simulation, "only the primary locale is simulated")
}

func TestDeploySimulatesFallbackLocale(t *testing.T) {
	h := newHarness(t, smapitest.Options{})

	req := h.request(deploy.State{})
	req.PrimaryLocale = "de-DE"

	res, err := h.deployer.Deploy(t.Context(), req)
	require.NoError(t, err)

	require.NotNil(t, res.Locales["en-GB"].Simulation, "a primary locale outside the package falls back to the first package locale")
	assert.Nil(t, res.Locales["en-US"].Simulation)
	assert.NotContains(t, res.Locales, "de-DE")
}

func TestDeploySimulationWaitsForTheModel(t *testing.T) {
	// The simulator doesn't know a just-enabled model for a moment.
	h := newHarness(t, smapitest.Options{UnresolvedSimulations: 2})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
	require.NotNil(t, res.Locales["en-GB"].Simulation)
	assert.True(t, res.Locales["en-GB"].Simulation.Invoked)
}

func TestDeploySimulationGivesUp(t *testing.T) {
	h := newHarness(t, smapitest.Options{UnresolvedSimulations: 100})

	res, err := h.deployer.Deploy(t.Context(), h.request(deploy.State{}))
	require.NoError(t, err, "a failed smoke test is a warning, not a failed deploy")
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "did not resolve to any intent")
}
