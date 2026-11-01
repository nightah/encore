package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/skillpkg"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

// liveFake starts a fake SMAPI and points the live tools' credentials at it, returning the flags that reach it.
func liveFake(t *testing.T) (*smapitest.Server, []string) {
	t.Helper()

	fake := smapitest.New(t, smapitest.Options{})
	o := fake.Options()

	t.Setenv(envClientID, o.ClientID)
	t.Setenv(envClientSecret, o.ClientSecret)
	t.Setenv(envRefreshToken, o.RefreshToken)
	t.Setenv(envSkillID, "")

	return fake, []string{"--smapi-url", fake.URL, "--token-url", fake.Endpoint().TokenURL}
}

func fixtureDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600))
	}

	return dir
}

const enGBFixture = `locale: en-GB
cases:
  - {utterance: "play the movie dune", intent: WatchMovie, slots: {Movie: dune}}
  - {utterance: "louder", intent: VolumeUp, pending: "added since the last deploy"}
`

func TestGenNLU(t *testing.T) {
	fake, flags := liveFake(t)
	skill := fake.AddSkill(smapitest.DefaultVendorID, nil)
	fake.Profile("en-GB", "play the movie dune", smapitest.NLUAnswer{Intent: "WatchMovie", Slots: map[string]string{"Movie": "dune"}})

	dir := fixtureDir(t, map[string]string{"en-GB.yaml": enGBFixture, "de-DE.yaml": "locale: de-DE\ncases:\n  - {utterance: lauter, intent: VolumeUp}\n"})

	out, _, err := runCLI(t, append([]string{"gen", "nlu", "--fixtures", dir, "--skill", skill, "--locale", "en-GB"}, flags...)...)
	require.NoError(t, err, "a pending row that still fails doesn't fail the run")
	assert.Contains(t, out, `PENDING en-GB "louder"`)
	assert.Contains(t, out, "1 passed, 0 failed, 1 pending, 0 fixed\n")

	t.Setenv(envSkillID, skill)

	out, _, err = runCLI(t, append([]string{"gen", "nlu", "--fixtures", dir, "--verbose"}, flags...)...)
	require.ErrorIs(t, err, errSilent, "the German row fails")
	assert.Contains(t, out, `FAIL    de-DE "lauter": got no intent, want VolumeUp`)
	assert.Contains(t, out, `PASS    en-GB "play the movie dune"`)
}

func TestGenNLURefuses(t *testing.T) {
	_, flags := liveFake(t)
	dir := fixtureDir(t, map[string]string{"en-GB.yaml": enGBFixture})

	_, _, err := runCLI(t, append([]string{"gen", "nlu", "--fixtures", dir}, flags...)...)
	require.ErrorContains(t, err, "name the skill with --skill or SMAPI_SKILL_ID")

	_, _, err = runCLI(t, append([]string{"gen", "nlu", "--fixtures", dir, "--skill", "s", "--locale", "fr-FR"}, flags...)...)
	require.ErrorContains(t, err, "no NLU fixture for fr-FR")

	_, _, err = runCLI(t, "gen", "nlu", "--fixtures", fixtureDir(t, map[string]string{"en-GB.yaml": "locale: en-GB\ncases:\n  - {utterance: louder, intent: Louder}\n"}))
	require.ErrorContains(t, err, `no intent "Louder" in the model`)

	_, _, err = runCLI(t, "gen", "nlu", "--fixtures", fixtureDir(t, map[string]string{"en-GB.yaml": "locale: en-AU\n"}))
	require.ErrorContains(t, err, "named for")

	_, _, err = runCLI(t, "gen", "nlu", "--fixtures", t.TempDir())
	require.ErrorContains(t, err, "no NLU fixtures in")

	t.Setenv(envRefreshToken, "")

	_, _, err = runCLI(t, "gen", "nlu", "--fixtures", dir, "--skill", "s")
	require.ErrorContains(t, err, "set SMAPI_CLIENT_ID, SMAPI_CLIENT_SECRET and SMAPI_REFRESH_TOKEN")

	_, _, err = runCLI(t, "gen", "open", "--skill", "s")
	require.ErrorContains(t, err, "set SMAPI_CLIENT_ID")
}

func TestGenOpen(t *testing.T) {
	fake, flags := liveFake(t)
	skill := deployedSkill(t, fake, "en-GB", "de-DE")

	out, stderr, err := runCLI(t, append([]string{"gen", "open", "--skill", skill, "--locale", "en-GB", "--locale", "de-DE", "--locale", "es-ES"}, flags...)...)
	require.NoError(t, err)

	assert.Contains(t, out, "REACHED     en-GB\n  “open encore”: reached the skill\n")
	assert.Contains(t, out, "NOT REACHED de-DE\n  “öffne encore”: Alexa said “Sorry, I don't know that.”\n")
	assert.Contains(t, out, "REFUSED     es-ES\n  “abre encore”: smapi: simulate: HTTP 400: locale es-ES is not supported for simulation\n")
	assert.Contains(t, out, "reached: en-GB\nrefused by the simulator: es-ES\nno phrase got through: de-DE\n")
	assert.Contains(t, stderr, "open check", "progress is logged")

	_, _, err = runCLI(t, append([]string{"gen", "open", "--skill", skill, "--locale", "xx-XX"}, flags...)...)
	require.ErrorContains(t, err, "unknown locale xx-XX")
}

func TestGenOpenEveryLocaleByDefault(t *testing.T) {
	fake, flags := liveFake(t)

	out, _, err := runCLI(t, append([]string{"gen", "open", "--skill", fake.AddSkill(smapitest.DefaultVendorID, nil)}, flags...)...)
	require.NoError(t, err)
	assert.Contains(t, out, "reached: none\n")
	assert.Contains(t, out, "REFUSED     pt-BR\n")
	assert.Contains(t, out, "NOT REACHED ja-JP\n  “アンコールを開いて”: smapi: simulate: HTTP 409: skill is not enabled for testing\n")
}

func TestWriteOpenChecksReportsWriteErrors(t *testing.T) {
	checks := []deploy.OpenCheck{{Locale: "en-GB", Tries: []deploy.OpenTry{{Phrase: "open encore", Simulation: &smapi.Simulation{Invoked: true}}}}}

	for n := range 3 {
		require.Error(t, writeOpenChecks(&failAfter{n: n}, checks), n)
	}

	assert.Equal(t, "none", list(nil))
}

type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n == 0 {
		return 0, assert.AnError
	}

	f.n--

	return len(p), nil
}

// deployedSkill deploys locales to the fake and enables the skill, as Encore's own deploy does.
func deployedSkill(t *testing.T, fake *smapitest.Server, locales ...string) string {
	t.Helper()

	o := fake.Options()

	lc, err := lwa.New(lwa.Config{Credentials: lwa.Credentials{ClientID: o.ClientID, ClientSecret: o.ClientSecret}, Endpoint: fake.Endpoint()})
	require.NoError(t, err)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: o.RefreshToken}))

	c, err := smapi.New(lc.TokenSource(store), smapi.Options{BaseURL: fake.URL, Poll: smapi.PollConfig{Interval: time.Millisecond, MaxInterval: time.Millisecond, Timeout: 10 * time.Second}})
	require.NoError(t, err)

	no, yes := false, true

	res, err := deploy.New(c, loadSources(t)).Deploy(t.Context(), deploy.Request{Package: skillpkg.Options{
		Locales:  locales,
		Endpoint: skillpkg.Endpoint{URI: "https://encore.example.com/api/v1/alexa"},
		Privacy:  skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no},
	}})
	require.NoError(t, err)

	return res.State.SkillID
}

// runCLIWith runs the CLI with ctx and out in place of the test's context and a buffer.
func runCLIWith(ctx context.Context, t *testing.T, out io.Writer, args ...string) error {
	t.Helper()

	t.Setenv("ENCORE_CONFIG", "")
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())

	cmd := NewRootCommand(out, io.Discard)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(""))

	return cmd.ExecuteContext(ctx)
}

// A run that is stopped, or can't write its report, says so instead of reporting a pass.
func TestGenNLUFailsWhenStoppedOrUnwritable(t *testing.T) {
	fake, flags := liveFake(t)
	skill := fake.AddSkill(smapitest.DefaultVendorID, nil)
	args := append([]string{"gen", "nlu", "--fixtures", fixtureDir(t, map[string]string{"en-GB.yaml": enGBFixture}), "--skill", skill}, flags...)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, runCLIWith(ctx, t, io.Discard, args...), context.Canceled)
	require.ErrorIs(t, runCLIWith(t.Context(), t, &failAfter{}, args...), assert.AnError)
}

func TestGenOpenFailsWhenStopped(t *testing.T) {
	fake, flags := liveFake(t)
	skill := deployedSkill(t, fake, "en-GB")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, runCLIWith(ctx, t, io.Discard, append([]string{"gen", "open", "--skill", skill, "--locale", "en-GB"}, flags...)...), context.Canceled)
}

func TestGenDeploy(t *testing.T) {
	fake, flags := liveFake(t)
	skill := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-US": ciSkillName})
	flags = append(flags, "--poll-interval", "1ms")

	out, stderr, err := runCLI(t, append([]string{"gen", "deploy", "--skill", skill, "--invocation", "kodi", "--locale", "en-GB", "--locale", "ja-JP"}, flags...)...)
	require.NoError(t, err)
	assert.Contains(t, out, "SUCCEEDED   en-GB\nSUCCEEDED   ja-JP\n")
	assert.Contains(t, stderr, "Updating skill "+skill, "the named skill is deployed to, not one looked up by name")

	sk, ok := fake.Skill(skill)
	require.True(t, ok)
	assert.True(t, sk.Enabled)
	assert.Equal(t, map[string]string{"en-GB": ciSkillName, "ja-JP": ciSkillName}, sk.Names)
	assert.Contains(t, string(sk.Manifest), ciEndpoint)
	assert.Contains(t, string(sk.Models["en-GB"]), `"invocationName":"kodi"`)
	assert.Equal(t, 1, fake.SkillCount(), "no other skill is created")

	out, _, err = runCLI(t, append([]string{"gen", "open", "--skill", skill, "--invocation", "kodi", "--locale", "en-GB"}, flags...)...)
	require.NoError(t, err)
	assert.Contains(t, out, "REACHED     en-GB\n  “open kodi”: reached the skill\n", "gen open checks what gen deploy deployed")

	t.Setenv(envVendorID, smapitest.DefaultVendorID)
	fake.FailLocale("en-GB", "a sample conflicts with another intent")

	out, _, err = runCLI(t, append([]string{"gen", "deploy", "--skill", skill, "--invocation", "encore", "--locale", "en-GB"}, flags...)...)
	require.ErrorIs(t, err, deploy.ErrBuildFailed)
	assert.Contains(t, out, "FAILED      en-GB\n  INVALID_INTERACTION_MODEL: a sample conflicts with another intent\n")
}

func TestGenDeployRefuses(t *testing.T) {
	fake, flags := liveFake(t)
	own := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-US": "Encore"})
	mixed := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-GB": ciSkillName, "en-US": "Encore"})
	unnamed := fake.AddSkill(smapitest.DefaultVendorID, nil)

	deployTo := func(skill string, extra ...string) error {
		_, _, err := runCLI(t, append(append([]string{"gen", "deploy", "--skill", skill, "--invocation", "encore"}, flags...), extra...)...)

		return err
	}

	require.ErrorContains(t, deployTo(own), "is named “Encore” in en-US, not “Encore NLU CI”")
	require.ErrorContains(t, deployTo(mixed), "is named “Encore” in en-US")
	require.ErrorContains(t, deployTo(unnamed), "has no name")
	require.ErrorContains(t, deployTo("amzn1.ask.skill.missing"), "has no skill amzn1.ask.skill.missing")
	require.ErrorContains(t, deployTo(own, "--vendor", "OTHER"), "vendor OTHER has no skill")
	require.ErrorContains(t, deployTo(own, "--locale", "xx-XX"), "unknown locale xx-XX")
	require.ErrorContains(t, deployTo(own, "--invocation", "alexa"), `--invocation is encore or kodi, not "alexa"`)
	require.ErrorContains(t, deployTo(""), "name the skill with --skill")

	_, _, err := runCLI(t, append([]string{"gen", "deploy", "--skill", own}, flags...)...)
	require.ErrorContains(t, err, `required flag(s) "invocation" not set`)

	sk, _ := fake.Skill(own)
	assert.Zero(t, sk.Imports, "a refused skill is never touched")
	assert.Equal(t, 3, fake.SkillCount())
}

func TestGenDeployNeedsOneVendor(t *testing.T) {
	for _, vendors := range [][]smapitest.Vendor{{}, {{ID: "A"}, {ID: "B"}}} {
		fake := smapitest.New(t, smapitest.Options{Vendors: vendors})
		o := fake.Options()

		t.Setenv(envClientID, o.ClientID)
		t.Setenv(envClientSecret, o.ClientSecret)
		t.Setenv(envRefreshToken, o.RefreshToken)
		t.Setenv(envVendorID, "")

		_, _, err := runCLI(t, "gen", "deploy", "--skill", "s", "--invocation", "encore", "--smapi-url", fake.URL, "--token-url", fake.Endpoint().TokenURL)
		require.ErrorContains(t, err, fmt.Sprintf("the account has %d developer accounts", len(vendors)))
	}

	fake, flags := liveFake(t)
	fake.Inject(http.StatusForbidden, 1)

	_, _, err := runCLI(t, append([]string{"gen", "deploy", "--skill", "s", "--invocation", "encore"}, flags...)...)
	require.ErrorContains(t, err, "403", "a vendor lookup that fails says why")
}

func TestWriteDeployReportsWriteErrors(t *testing.T) {
	res := &deploy.Result{
		Locales:  map[string]deploy.LocaleStatus{"en-GB": {Status: smapi.StatusSucceeded, Warnings: []smapi.Problem{{Message: "a sample is long"}}}},
		Warnings: []string{"a title was dropped"},
	}

	for n := range 3 {
		require.Error(t, writeDeploy(&failAfter{n: n}, res), n)
	}

	var out strings.Builder

	require.NoError(t, writeDeploy(&out, res))
	assert.Equal(t, "SUCCEEDED   en-GB\n  a sample is long\nwarning: a title was dropped\n", out.String())
}

// A deploy that can't list the skills, or can't write its report, says so instead of reporting a pass.
func TestGenDeployFailsWhenUnlistedOrUnwritable(t *testing.T) {
	fake, flags := liveFake(t)
	skill := fake.AddSkill(smapitest.DefaultVendorID, map[string]string{"en-AU": ciSkillName})
	args := append([]string{"gen", "deploy", "--skill", skill, "--invocation", "encore", "--locale", "en-AU", "--vendor", smapitest.DefaultVendorID, "--poll-interval", "1ms"}, flags...)

	fake.Inject(http.StatusForbidden, 1)

	_, _, err := runCLI(t, args...)
	require.ErrorContains(t, err, "403")

	require.ErrorIs(t, runCLIWith(t.Context(), t, &failAfter{}, args...), assert.AnError)

	sk, _ := fake.Skill(skill)
	assert.True(t, sk.Enabled, "the deploy itself went through")
}
