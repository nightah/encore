package cli

import (
	"context"
	"io"
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
