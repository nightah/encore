package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/smapitest"
	"github.com/nightah/encore/internal/store"
)

// simulatedSkill deploys and enables a skill in the fake, answering the live TV phrases as a Kodi instance with Australian channels would.
func simulatedSkill(t *testing.T, fake *smapitest.Server) string {
	t.Helper()

	skill := deployedSkill(t, fake, "en-AU", "en-US")
	ended, open := true, false

	fake.Answer("en-AU", "ask encore what's on seven", smapitest.Answer{
		Intent: "WhatsOn", Slots: map[string]string{"Channel": "7"}, Resolved: map[string]string{"Channel": "Seven"},
		Speech: "On Seven now: The Morning Show, until 12:00 pm.", EndSession: &ended,
	})
	fake.Answer("en-AU", "ask encore to switch to nine go", smapitest.Answer{Intent: "WatchChannel", Slots: map[string]string{"Channel": "9 go"}, Unresolved: true})
	fake.Answer("en-AU", "ask encore to open the tv guide", smapitest.Answer{Intent: "OpenTVGuide", Slots: map[string]string{"LiveView": "tv guide"}, Resolved: map[string]string{"LiveView": "guide"}, Speech: "<speak><audio src=\"soundbank://x\"/></speak>"})
	fake.Answer("en-AU", "ask encore to play dune", smapitest.Answer{Intent: "PlayMedia", Slots: map[string]string{"Query": "dune"}, Speech: "Which one?", Reprompt: "Say the first or the second.", EndSession: &open, Directives: []string{"Alexa.Presentation.APL.RenderDocument"}})
	fake.Answer("en-US", "ask encore what's on seven", smapitest.Answer{Intent: "WhatsOn", Slots: map[string]string{"Channel": "7"}, Speech: "en-US answer"})

	return skill
}

func TestGenSimulate(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)

	out, _, err := runCLI(t, append([]string{
		"gen", "simulate", "--skill", skill, "-l", "en-AU",
		"ask encore what's on seven", "ask encore to switch to nine go", "ask encore to play dune", "ask encore to dance",
	}, flags...)...)
	require.NoError(t, err, "a simulation that resolves no intent is reported, not a failure")

	assert.Equal(t, `“ask encore what's on seven”
  intent:  WhatsOn Channel="7" (Seven)
  skill:   reached, session ended
  Alexa:   “On Seven now: The Morning Show, until 12:00 pm.”
“ask encore to switch to nine go”
  failed:  `+smapitest.UnresolvedMessage+`
  weighed: WatchChannel Channel="9 go"
“ask encore to play dune”
  intent:  PlayMedia Query="dune"
  skill:   reached, session open, directives Alexa.Presentation.APL.RenderDocument
  reprompt: “<speak>Say the first or the second.</speak>”
  Alexa:   “Which one?”
“ask encore to dance”
  intent:  no intent
  skill:   not reached
  Alexa:   “Sorry, I don't know that.”
`, out)
	assert.Equal(t, []string{"FORCE_NEW_SESSION", "FORCE_NEW_SESSION", "FORCE_NEW_SESSION", "FORCE_NEW_SESSION"}, fake.SessionModes())
}

func TestGenSimulateSession(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)

	_, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "--session", "-l", "en-AU", "ask encore to play dune", "the first one"}, flags...)...)
	require.NoError(t, err)
	assert.Equal(t, []string{"FORCE_NEW_SESSION", "DEFAULT"}, fake.SessionModes(), "the first utterance opens a session the rest answer in")
}

func TestGenSimulateJSON(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)

	out, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "--json", "-l", "en-AU", "ask encore what's on seven"}, flags...)...)
	require.NoError(t, err)

	var res simResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "en-AU", res.Locale)
	assert.Equal(t, "WhatsOn", res.Simulation.Intent)
	assert.Equal(t, smapi.SimulationSlot{Value: "7", Resolved: "Seven"}, res.Simulation.Slots["Channel"])
	assert.Empty(t, res.Misses)
}

const simulateCases = `locale: en-AU
cases:
  - {utterance: "ask encore what's on seven", intent: WhatsOn, slots: {Channel: seven}, says: "^On Seven now: "}
  - {utterance: "ask encore to open the tv guide", intent: OpenTVGuide, slots: {LiveView: guide}}
  - {utterance: "ask encore what's on seven", locale: en-US, says: "en-US"}
  - {utterance: "ask encore to switch to nine go", intent: WatchChannel}
  - {utterance: "ask encore to play dune", intent: WatchMovie, slots: {Query: "dune", Movie: ""}, says: "^Playing"}
  - {utterance: "ask encore to open the tv guide", slots: {LiveView: channels}}
`

func TestGenSimulateFile(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)
	file := filepath.Join(fixtureDir(t, map[string]string{"cases.yaml": simulateCases}), "cases.yaml")

	out, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "-f", file}, flags...)...)
	require.ErrorIs(t, err, errSilent, "a failed case fails the command once it's reported")

	assert.Equal(t, `PASS  “ask encore what's on seven”
PASS  “ask encore to open the tv guide”
PASS  “ask encore what's on seven”
FAIL  “ask encore to switch to nine go”
  want intent WatchChannel; the utterance resolved to none of the skill's intents (WatchChannel Channel="9 go")
  failed:  `+smapitest.UnresolvedMessage+`
  weighed: WatchChannel Channel="9 go"
FAIL  “ask encore to play dune”
  want intent WatchMovie, got PlayMedia Query="dune"
  want slot Movie filled, but it's empty
  want a reply matching “^Playing”, got “Which one?”
  intent:  PlayMedia Query="dune"
  skill:   reached, session open, directives Alexa.Presentation.APL.RenderDocument
  reprompt: “<speak>Say the first or the second.</speak>”
  Alexa:   “Which one?”
FAIL  “ask encore to open the tv guide”
  want slot LiveView="channels", got "tv guide" (guide)
  intent:  OpenTVGuide LiveView="tv guide" (guide)
  skill:   reached, session left as it was
  Alexa:   “<speak><audio src="soundbank://x"/></speak>”
3 passed, 3 failed
`, out)

	out, _, err = runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "-f", file, "--json"}, flags...)...)
	require.ErrorIs(t, err, errSilent)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 6, "one line a case, and no tally")

	var res simResult
	require.NoError(t, json.Unmarshal([]byte(lines[3]), &res))
	assert.Equal(t, []string{`want intent WatchChannel; the utterance resolved to none of the skill's intents (WatchChannel Channel="9 go")`}, res.Misses)
}

func TestGenSimulateLocaleFlagOverridesTheFile(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)
	file := filepath.Join(fixtureDir(t, map[string]string{"cases.yaml": "locale: en-AU\ncases:\n  - {utterance: \"ask encore what's on seven\", says: en-US}\n"}), "cases.yaml")

	out, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "-f", file, "-l", "en-US"}, flags...)...)
	require.NoError(t, err)
	assert.Equal(t, "PASS  “ask encore what's on seven”\n1 passed, 0 failed\n", out)
}

func TestGenSimulateRefuses(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)
	dir := fixtureDir(t, map[string]string{
		"empty.yaml":    "locale: en-AU\ncases: []\n",
		"unknown.yaml":  "locale: en-AU\ncases:\n  - {utterance: hi, expect: x}\n",
		"blank.yaml":    "cases:\n  - {utterance: \" \"}\n",
		"badregex.yaml": "cases:\n  - {utterance: hi, says: \"(\"}\n",
	})

	testCases := []struct {
		name string
		have []string
		err  string
	}{
		{name: "ShouldRefuseNothingToSay", err: "give an utterance to say, or --file"},
		{name: "ShouldRefuseUtterancesAndAFile", have: []string{"-f", filepath.Join(dir, "empty.yaml"), "hi"}, err: "give utterances or --file, not both"},
		{name: "ShouldRefuseAMissingFile", have: []string{"-f", filepath.Join(dir, "none.yaml")}, err: "no such file"},
		{name: "ShouldRefuseAFileWithoutCases", have: []string{"-f", filepath.Join(dir, "empty.yaml")}, err: "no cases"},
		{name: "ShouldRefuseAnUnknownField", have: []string{"-f", filepath.Join(dir, "unknown.yaml")}, err: "field expect not found"},
		{name: "ShouldRefuseABlankUtterance", have: []string{"-f", filepath.Join(dir, "blank.yaml")}, err: "case 1 has no utterance"},
		{name: "ShouldRefuseABadRegex", have: []string{"-f", filepath.Join(dir, "badregex.yaml")}, err: "says: error parsing regexp"},
		{name: "ShouldRefuseALocaleTheSimulatorRejects", have: []string{"-l", "es-ES", "abre encore"}, err: "HTTP 400"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := runCLI(t, append(append([]string{"gen", "simulate", "--skill", skill}, tc.have...), flags...)...)
			require.ErrorContains(t, err, tc.err)
			assert.Empty(t, out)
		})
	}

	t.Setenv(envClientID, "")

	_, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "hi"}, flags...)...)
	require.ErrorContains(t, err, envClientID)
}

func TestGenSimulateEnablesASkillOnlyWhileItSimulates(t *testing.T) {
	fake, flags := liveFake(t)
	skill := fake.AddSkill(smapitest.DefaultVendorID, nil)

	_, stderr, err := runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "open encore"}, flags...)...)
	require.NoError(t, err)
	assert.Contains(t, stderr, "enabled the skill for testing while it's checked")

	sk, _ := fake.Skill(skill)
	assert.False(t, sk.Enabled)

	fake.InjectRoute("GET /v1/skills/{skillId}/stages/{stage}/enablement", http.StatusForbidden)

	_, _, err = runCLI(t, append([]string{"gen", "simulate", "--skill", skill, "open encore"}, flags...)...)
	require.ErrorContains(t, err, "403")
}

func TestGenSimulateFailsWhenStoppedOrUnwritable(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)

	file := filepath.Join(fixtureDir(t, map[string]string{"cases.yaml": simulateCases}), "cases.yaml")

	testCases := []struct {
		name     string
		have     []string
		canceled bool
		out      io.Writer
		err      error
	}{
		{name: "ShouldFailWhenStopped", have: []string{"-l", "en-AU", "ask encore what's on seven"}, canceled: true, out: io.Discard, err: context.Canceled},
		{name: "ShouldFailWhenTheReportCantBeWritten", have: []string{"-l", "en-AU", "ask encore what's on seven"}, out: &failAfter{}, err: assert.AnError},
		{name: "ShouldFailWhenTheJSONCantBeWritten", have: []string{"--json", "-l", "en-AU", "ask encore what's on seven"}, out: &failAfter{}, err: assert.AnError},
		{name: "ShouldFailWhenTheFileReportCantBeWritten", have: []string{"-f", file}, out: &failAfter{}, err: assert.AnError},
		{name: "ShouldFailWhenTheTallyCantBeWritten", have: []string{"-f", file}, out: failOn("3 passed"), err: assert.AnError},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tc.canceled {
				cancel()
			}

			require.ErrorIs(t, runCLIWith(ctx, t, tc.out, append(append([]string{"gen", "simulate", "--skill", skill}, tc.have...), flags...)...), tc.err)
		})
	}

	disabled := fake.AddSkill(smapitest.DefaultVendorID, nil)

	err := runCLIWith(t.Context(), t, &failAfter{}, append([]string{"gen", "simulate", "--skill", disabled, "open encore"}, flags...)...)
	require.ErrorIs(t, err, assert.AnError)

	sk, _ := fake.Skill(disabled)
	assert.False(t, sk.Enabled, "a skill enabled only to simulate is disabled again when the report can't be written")
}

func TestReplyFallsBackToTheSkillsSpeech(t *testing.T) {
	responses := []smapi.SkillResponse{{Speech: "Switching to Seven."}, {}, {Speech: "Done."}}

	testCases := []struct {
		name     string
		have     *smapi.Simulation
		expected string
	}{
		{name: "ShouldReplyWithTheSkillsSpeechWhenAlexaCaptionedNothing", have: &smapi.Simulation{Responses: responses}, expected: "Switching to Seven. Done."},
		{name: "ShouldReplyWithTheCaptions", have: &smapi.Simulation{Responses: responses, Captions: []string{"Switching", "to Seven."}}, expected: "Switching to Seven."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, reply(tc.have))
		})
	}
}

// encoreDataDir makes a data directory as an Encore connected to the fake and deployed as skill leaves it, in en-AU only, and points the CLI at it. The SMAPI_* variables are cleared, so only the data directory can name the skill.
func encoreDataDir(t *testing.T, fake *smapitest.Server, skill string) string {
	t.Helper()

	dir := t.TempDir()
	ctx := t.Context()
	o := fake.Options()

	db, err := store.Open(ctx, store.Options{Path: store.DefaultPath(dir)})
	require.NoError(t, err)

	defer db.Close()

	sec, err := secrets.Open(ctx, db.SecretRows(), secrets.Options{DataDir: dir})
	require.NoError(t, err)

	settings := db.Settings()
	// The keys the server keeps them under, which api.ReadSkillAccess reads.
	require.NoError(t, settings.Set(ctx, "skill.lwa", map[string]string{"client_id": o.ClientID}))
	require.NoError(t, settings.Set(ctx, "skill.deploy_state", map[string]string{"skillId": skill}))
	require.NoError(t, settings.Set(ctx, "config.overrides", map[string]any{"alexa.locales": []string{"en-AU"}}))
	require.NoError(t, sec.Put(ctx, "skill/lwa/client_secret", []byte(o.ClientSecret)))
	require.NoError(t, sec.Put(ctx, "skill/lwa/token", []byte(`{"refresh_token": "`+o.RefreshToken+`"}`)))

	t.Setenv("ENCORE_DATA_DIR", dir)

	for _, env := range []string{envClientID, envClientSecret, envRefreshToken, envSkillID} {
		t.Setenv(env, "")
	}

	return dir
}

func TestGenSimulateEncoresOwnSkill(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)
	encoreDataDir(t, fake, skill)

	out, _, err := runCLI(t, append([]string{"gen", "simulate", "ask encore what's on seven"}, flags...)...)
	require.NoError(t, err, "the skill and credentials come from the data directory")
	assert.Contains(t, out, "On Seven now", "in the skill's own locale, en-AU, not the en-US default")

	other := fake.AddSkill(smapitest.DefaultVendorID, nil)

	_, stderr, err := runCLI(t, append([]string{"gen", "simulate", "--skill", other, "open encore"}, flags...)...)
	require.NoError(t, err, "--skill names another skill, still reached with Encore's credentials")
	assert.Contains(t, stderr, other, "the other skill is enabled while it's simulated")
}

func TestGenSimulateFallsBackToTheEnvironment(t *testing.T) {
	fake, flags := liveFake(t)
	skill := simulatedSkill(t, fake)
	o := fake.Options()

	testCases := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{name: "ShouldUseTheEnvironmentWithoutADataDirectory", setup: func(*testing.T) {}},
		{
			name: "ShouldUseTheEnvironmentWhenNotConnected",
			setup: func(t *testing.T) {
				dir := encoreDataDir(t, fake, skill)
				db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir)})
				require.NoError(t, err)
				require.NoError(t, db.Settings().Delete(t.Context(), "skill.deploy_state"))
				require.NoError(t, db.Close())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENCORE_DATA_DIR", t.TempDir())
			tc.setup(t)
			t.Setenv(envClientID, o.ClientID)
			t.Setenv(envClientSecret, o.ClientSecret)
			t.Setenv(envRefreshToken, o.RefreshToken)
			t.Setenv(envSkillID, skill)

			out, _, err := runCLI(t, append([]string{"gen", "simulate", "-l", "en-AU", "ask encore what's on seven"}, flags...)...)
			require.NoError(t, err)
			assert.Contains(t, out, "On Seven now")
		})
	}
}

func TestGenSimulateNeedsASkill(t *testing.T) {
	_, flags := liveFake(t)

	testCases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name: "ShouldRefuseWithNothingToNameTheSkill",
			setup: func(t *testing.T) string {
				t.Setenv(envClientID, "")

				return "no such file"
			},
		},
		{
			name: "ShouldRefuseAStatelessEncore",
			setup: func(t *testing.T) string {
				t.Setenv("ENCORE_STATELESS", "true")
				t.Setenv("ENCORE_STORAGE_BACKEND", "webdav")
				t.Setenv("ENCORE_STORAGE_WEBDAV_URL", "https://dav.example.com/encore")
				t.Setenv("ENCORE_SECRETS_KEY", "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s=")

				return "stateless Encore keeps no data directory"
			},
		},
		{
			name: "ShouldRefuseWithoutASecretsKey",
			setup: func(t *testing.T) string {
				fake, _ := liveFake(t)
				dir := encoreDataDir(t, fake, "amzn1.ask.skill.x")
				require.NoError(t, os.Remove(filepath.Join(dir, secrets.KeyFileName)))

				return "key"
			},
		},
		{
			name: "ShouldRefuseABadConfig",
			setup: func(t *testing.T) string {
				t.Setenv("ENCORE_LOG_LEVEL", "shouting")

				return "shouting"
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.setup(t)

			_, _, err := runCLI(t, append([]string{"gen", "simulate", "hi"}, flags...)...)
			require.Error(t, err)
			assert.ErrorContains(t, err, want)
		})
	}
}

func TestGenSimulateAnotherSkillTakesNoStoredLocale(t *testing.T) {
	fake, flags := liveFake(t)
	other := simulatedSkill(t, fake)
	encoreDataDir(t, fake, fake.AddSkill(smapitest.DefaultVendorID, nil))

	out, _, err := runCLI(t, append([]string{"gen", "simulate", "--skill", other, "ask encore what's on seven"}, flags...)...)
	require.NoError(t, err)
	assert.Contains(t, out, "en-US answer", "the stored skill's en-AU is its own, so another skill is said to in "+simulateLocale)
}

func TestWriteSimulationReachedWithoutAResponse(t *testing.T) {
	testCases := []struct {
		name     string
		have     *smapi.Simulation
		expected string
	}{
		{name: "ShouldSayReachedWhenTheSimulatorReachedTheEndpointWhateverCameBack", have: &smapi.Simulation{Status: smapi.SimulationSuccessful, Invoked: true}, expected: "  intent:  no intent\n  skill:   reached, but its response couldn't be read\n"},
		{name: "ShouldSayNotReached", have: &smapi.Simulation{Status: smapi.SimulationSuccessful}, expected: "  intent:  no intent\n  skill:   not reached\n"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder

			writeSimulation(&b, tc.have)
			assert.Equal(t, tc.expected, b.String())
		})
	}
}
