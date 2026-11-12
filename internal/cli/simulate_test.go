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

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"nothing":   {nil, "give an utterance to say, or --file"},
		"both":      {[]string{"-f", filepath.Join(dir, "empty.yaml"), "hi"}, "give utterances or --file, not both"},
		"missing":   {[]string{"-f", filepath.Join(dir, "none.yaml")}, "no such file"},
		"empty":     {[]string{"-f", filepath.Join(dir, "empty.yaml")}, "no cases"},
		"unknown":   {[]string{"-f", filepath.Join(dir, "unknown.yaml")}, "field expect not found"},
		"blank":     {[]string{"-f", filepath.Join(dir, "blank.yaml")}, "case 1 has no utterance"},
		"bad regex": {[]string{"-f", filepath.Join(dir, "badregex.yaml")}, "says: error parsing regexp"},
		"locale":    {[]string{"-l", "es-ES", "abre encore"}, "HTTP 400"},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runCLI(t, append(append([]string{"gen", "simulate", "--skill", skill}, tc.args...), flags...)...)
			require.ErrorContains(t, err, tc.want)
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

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, runCLIWith(ctx, t, io.Discard, append([]string{"gen", "simulate", "--skill", skill, "-l", "en-AU", "ask encore what's on seven"}, flags...)...), context.Canceled)

	file := filepath.Join(fixtureDir(t, map[string]string{"cases.yaml": simulateCases}), "cases.yaml")

	for _, args := range [][]string{
		{"-l", "en-AU", "ask encore what's on seven"},
		{"--json", "-l", "en-AU", "ask encore what's on seven"},
		{"-f", file},
	} {
		err := runCLIWith(t.Context(), t, &failAfter{}, append(append([]string{"gen", "simulate", "--skill", skill}, args...), flags...)...)
		require.ErrorIs(t, err, assert.AnError, args)
	}

	err := runCLIWith(t.Context(), t, failOn("3 passed"), append([]string{"gen", "simulate", "--skill", skill, "-f", file}, flags...)...)
	require.ErrorIs(t, err, assert.AnError, "nor the tally")
}

func TestReplyFallsBackToTheSkillsSpeech(t *testing.T) {
	sim := &smapi.Simulation{Responses: []smapi.SkillResponse{{Speech: "Switching to Seven."}, {}, {Speech: "Done."}}}
	assert.Equal(t, "Switching to Seven. Done.", reply(sim), "Alexa captioned nothing, so the skill's own speech is the reply")

	sim.Captions = []string{"Switching", "to Seven."}
	assert.Equal(t, "Switching to Seven.", reply(sim))
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

	for name, setup := range map[string]func(t *testing.T){
		"no data directory": func(*testing.T) {},
		"not connected": func(t *testing.T) {
			dir := encoreDataDir(t, fake, skill)
			db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir)})
			require.NoError(t, err)
			require.NoError(t, db.Settings().Delete(t.Context(), "skill.deploy_state"))
			require.NoError(t, db.Close())
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ENCORE_DATA_DIR", t.TempDir())
			setup(t)
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

	for name, setup := range map[string]func(t *testing.T) string{
		"nothing": func(t *testing.T) string {
			t.Setenv(envClientID, "")

			return "no such file"
		},
		"a stateless Encore": func(t *testing.T) string {
			t.Setenv("ENCORE_STATELESS", "true")
			t.Setenv("ENCORE_STORAGE_BACKEND", "webdav")
			t.Setenv("ENCORE_STORAGE_WEBDAV_URL", "https://dav.example.com/encore")
			t.Setenv("ENCORE_SECRETS_KEY", "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s=")

			return "stateless Encore keeps no data directory"
		},
		"no secrets key": func(t *testing.T) string {
			fake, _ := liveFake(t)
			dir := encoreDataDir(t, fake, "amzn1.ask.skill.x")
			require.NoError(t, os.Remove(filepath.Join(dir, secrets.KeyFileName)))

			return "key"
		},
		"a bad config": func(t *testing.T) string {
			t.Setenv("ENCORE_LOG_LEVEL", "shouting")

			return "shouting"
		},
	} {
		t.Run(name, func(t *testing.T) {
			want := setup(t)

			_, _, err := runCLI(t, append([]string{"gen", "simulate", "hi"}, flags...)...)
			require.Error(t, err)
			assert.ErrorContains(t, err, want)
		})
	}
}
