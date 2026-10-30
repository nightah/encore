package nlu_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/nlu"
	"github.com/nightah/encore/internal/smapi/smapitest"
)

// runner profiles against a fake SMAPI with a fast retry policy and no wait between reprobes.
func runner(t *testing.T) (*nlu.Runner, *smapitest.Server) {
	t.Helper()

	fake := smapitest.New(t, smapitest.Options{})
	o := fake.Options()

	lc, err := lwa.New(lwa.Config{Credentials: lwa.Credentials{ClientID: o.ClientID, ClientSecret: o.ClientSecret}, Endpoint: fake.Endpoint()})
	require.NoError(t, err)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: o.RefreshToken}))

	c, err := smapi.New(lc.TokenSource(store), smapi.Options{
		BaseURL: fake.URL,
		Retry:   smapi.RetryPolicy{Base: time.Millisecond, Max: 2 * time.Millisecond, MaxRetryAfter: time.Millisecond},
	})
	require.NoError(t, err)

	return &nlu.Runner{Client: c, SkillID: fake.AddSkill(smapitest.DefaultVendorID, nil), Wait: time.Millisecond}, fake
}

func sources(t *testing.T) *skillmodel.Sources {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	return src
}

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"en-GB.yaml": {Data: []byte("locale: en-GB\ncases:\n  - utterance: volume up\n    intent: VolumeUp\n")},
		"de-DE.yaml": {Data: []byte("locale: de-DE\ncases:\n  - utterance: lauter\n    intent: VolumeUp\n    pending: next deploy\n")},
		"README.md":  {Data: []byte("not a fixture")},
	}

	got, err := nlu.Load(fsys)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "de-DE", got[0].Locale)
	assert.Equal(t, []nlu.Case{{Utterance: "lauter", Intent: "VolumeUp", Pending: "next deploy"}}, got[0].Cases)
	assert.Equal(t, "en-GB", got[1].Locale)
}

func TestLoadRejects(t *testing.T) {
	for name, data := range map[string]string{
		"a file named for another locale": "locale: en-AU\ncases: []\n",
		"an unknown field":                "locale: en-GB\ncases:\n  - utterance: volume up\n    intnet: VolumeUp\n",
		"broken yaml":                     "locale: [\n",
	} {
		_, err := nlu.Load(fstest.MapFS{"en-GB.yaml": {Data: []byte(data)}})
		require.Error(t, err, name)
	}

	_, err := nlu.LoadFile(fstest.MapFS{}, "en-GB.yaml")
	require.Error(t, err)

	_, err = nlu.Load(fstest.MapFS{"[.yaml": {Data: []byte("locale: x\n")}})
	require.Error(t, err)
}

func TestCheck(t *testing.T) {
	src := sources(t)

	ok := nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{{Utterance: "play the movie dune", Intent: "WatchMovie", Slots: map[string]string{"Movie": "dune"}}}}
	require.NoError(t, ok.Check(src))

	bad := nlu.Fixture{Locale: "en-XX", Cases: []nlu.Case{
		{Utterance: "", Intent: "VolumeUp"},
		{Utterance: "Volume up", Intent: "VolumeUp"},
		{Utterance: "volume up ", Intent: "VolumeUp"},
		{Utterance: "make it louder", Intent: "Louder"},
		{Utterance: "play dune in the attic", Intent: "WatchMovie", Slots: map[string]string{"Film": "dune", "Room": "attic"}},
	}}

	err := bad.Check(src)
	require.Error(t, err)

	for _, want := range []string{
		"en-XX: no such locale in the sources",
		`en-XX row 1 (""): no utterance`,
		`en-XX row 3 ("volume up "): utterance repeated`,
		`en-XX row 4 ("make it louder"): no intent "Louder" in the model`,
		`en-XX row 5 ("play dune in the attic"): WatchMovie has no slot Film`,
	} {
		assert.Contains(t, err.Error(), want)
	}

	assert.NotContains(t, err.Error(), "no slot Room")
}

func TestMissing(t *testing.T) {
	src := sources(t)

	f := nlu.Fixture{Locale: "en-GB"}
	for _, in := range src.Model.Intents {
		if in.Name != "VolumeUp" && in.Name != "Mute" {
			f.Cases = append(f.Cases, nlu.Case{Utterance: in.Name, Intent: in.Name})
		}
	}

	assert.Equal(t, []string{"VolumeUp", "Mute"}, f.Missing(src))
}

func TestRunComparesIntentAndSlots(t *testing.T) {
	r, fake := runner(t)

	fake.Profile("en-GB", "play the movie dune in the lounge", smapitest.NLUAnswer{
		Intent: "WatchMovie", Slots: map[string]string{"Movie": "Dune", "Room": "lounge"}, Resolved: map[string]string{"Room": "living_room"},
	})
	fake.Profile("en-GB", "play dune", smapitest.NLUAnswer{Intent: "PlayMedia", Slots: map[string]string{"Query": "dune"}, Considered: []string{"WatchMovie", "ListenToSong", "WatchMusicVideo", "ListenToAlbum"}})
	fake.Profile("en-GB", "watch the movie", smapitest.NLUAnswer{Intent: "WatchMovie", Slots: map[string]string{"Movie": " "}})
	fake.Profile("en-GB", "play the movie heat", smapitest.NLUAnswer{Intent: "WatchMovie", Slots: map[string]string{"Movie": "wheat"}})

	f := nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{
		{Utterance: "play the movie dune in the lounge", Intent: "WatchMovie", Slots: map[string]string{"Movie": "dune", "Room": "living_room"}},
		{Utterance: "play dune", Intent: "WatchMovie", Slots: map[string]string{"Movie": "dune"}},
		{Utterance: "watch the movie", Intent: "WatchMovie", Slots: map[string]string{"Movie": "", "Room": ""}},
		{Utterance: "play the movie heat", Intent: "WatchMovie", Slots: map[string]string{"Movie": "heat"}},
	}}

	res, err := r.Run(t.Context(), f)
	require.NoError(t, err)
	require.Len(t, res, 4)

	assert.Equal(t, nlu.OutcomePass, res[0].Outcome(), "the words match ignoring case, and a resolved ID matches too: %v", res[0].Problems)

	assert.Equal(t, nlu.OutcomeFail, res[1].Outcome())
	assert.Equal(t, []string{"got PlayMedia, want WatchMovie"}, res[1].Problems)
	assert.Equal(t, []string{"WatchMovie", "ListenToSong", "WatchMusicVideo"}, res[1].RunnersUp())

	assert.Equal(t, []string{"slot Movie is empty", "slot Room is empty"}, res[2].Problems)
	assert.Equal(t, []string{`slot Movie is "wheat", want "heat"`}, res[3].Problems)

	for _, r := range res {
		assert.Equal(t, 1, r.Probes)
		assert.Equal(t, "en-GB", r.Locale)
	}
}

func TestRunReprobesNoIntent(t *testing.T) {
	r, fake := runner(t)

	// Amazon's NLU answers no intent for a moment, then the real one.
	fake.Profile("de-DE", "lauter", smapitest.NLUAnswer{}, smapitest.NLUAnswer{Intent: "VolumeUp"})

	res, err := r.Run(t.Context(), nlu.Fixture{Locale: "de-DE", Cases: []nlu.Case{
		{Utterance: "lauter", Intent: "VolumeUp"},
		{Utterance: "bananen", Intent: "AMAZON.FallbackIntent"},
	}})
	require.NoError(t, err)

	assert.Equal(t, nlu.OutcomePass, res[0].Outcome())
	assert.Equal(t, 2, res[0].Probes)

	assert.Equal(t, nlu.OutcomeFail, res[1].Outcome())
	assert.Equal(t, []string{"got no intent, want AMAZON.FallbackIntent"}, res[1].Problems)
	assert.Equal(t, 3, res[1].Probes, "no intent counts only after the default two reprobes")
	assert.Equal(t, 3, fake.Profiled("de-DE", "bananen"))
	assert.Empty(t, res[1].RunnersUp())
}

func TestRunPending(t *testing.T) {
	r, fake := runner(t)
	r.Reprobes = 1

	fake.Profile("nl-NL", "speel de film dune", smapitest.NLUAnswer{Intent: "WatchMovie", Slots: map[string]string{"Movie": "dune"}})

	res, err := r.Run(t.Context(), nlu.Fixture{Locale: "nl-NL", Cases: []nlu.Case{
		{Utterance: "speel de film dune", Intent: "WatchMovie", Pending: "needs the next deploy"},
		{Utterance: "zoek naar dune", Intent: "PlayMedia", Pending: "needs the next deploy"},
	}})
	require.NoError(t, err)

	assert.Equal(t, nlu.OutcomeFixed, res[0].Outcome())
	assert.Equal(t, nlu.OutcomePending, res[1].Outcome())
	assert.Equal(t, 2, res[1].Probes)
	assert.Equal(t, nlu.Summary{Pending: 1, Fixed: 1}, nlu.Summarise(res))
	assert.False(t, nlu.Summarise(res).Failed(), "pending rows don't fail the run")
}

func TestRunProfileError(t *testing.T) {
	r, fake := runner(t)
	fake.Profile("en-GB", "mute", smapitest.NLUAnswer{Status: http.StatusForbidden})

	res, err := r.Run(t.Context(), nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{{Utterance: "mute", Intent: "Mute"}}})
	require.NoError(t, err)
	require.Error(t, res[0].Err)
	assert.Equal(t, http.StatusForbidden, smapi.StatusCode(res[0].Err))
	assert.Equal(t, nlu.OutcomeFail, res[0].Outcome())
	assert.Nil(t, res[0].Got)
	assert.Empty(t, res[0].RunnersUp())
	assert.True(t, nlu.Summarise(res).Failed())
}

func TestRunCancelled(t *testing.T) {
	r, _ := runner(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.Run(ctx, nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{{Utterance: "mute", Intent: "Mute"}, {Utterance: "unmute", Intent: "Mute"}}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunCancelledWhileWaitingToReprobe(t *testing.T) {
	r, fake := runner(t)
	r.Wait = time.Hour
	r.Parallel = 1

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		for fake.Profiled("en-GB", "bananas") == 0 {
			time.Sleep(time.Millisecond)
		}

		cancel()
	}()

	_, err := r.Run(ctx, nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{{Utterance: "bananas", Intent: "AMAZON.FallbackIntent"}}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestWrite(t *testing.T) {
	r, fake := runner(t)

	fake.Profile("en-GB", "volume up", smapitest.NLUAnswer{Intent: "VolumeUp"})
	fake.Profile("en-GB", "play dune", smapitest.NLUAnswer{Intent: "PlayMedia", Considered: []string{"WatchMovie"}})
	fake.Profile("en-GB", "mute", smapitest.NLUAnswer{Status: http.StatusForbidden})
	fake.Profile("en-GB", "louder", smapitest.NLUAnswer{Intent: "VolumeUp"})

	res, err := r.Run(t.Context(), nlu.Fixture{Locale: "en-GB", Cases: []nlu.Case{
		{Utterance: "volume up", Intent: "VolumeUp"},
		{Utterance: "play dune", Intent: "WatchMovie"},
		{Utterance: "mute", Intent: "Mute"},
		{Utterance: "louder", Intent: "VolumeUp", Pending: "added since the last deploy"},
		{Utterance: "quieter", Intent: "VolumeDown", Pending: "added since the last deploy"},
	}})
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, nlu.Write(&b, res, false))

	out := b.String()
	assert.NotContains(t, out, `"volume up"`, "passes are left out")
	assert.Contains(t, out, `FAIL    en-GB "play dune": got PlayMedia, want WatchMovie (also considered WatchMovie)`+"\n")
	assert.Contains(t, out, `FAIL    en-GB "mute": smapi: profile utterance: HTTP 403`)
	assert.Contains(t, out, `FIXED   en-GB "louder": passes now, so drop its pending mark`+"\n")
	assert.Contains(t, out, `PENDING en-GB "quieter": got no intent, want VolumeDown [pending: added since the last deploy] [asked 3 times]`+"\n")
	assert.Contains(t, out, "1 passed, 2 failed, 1 pending, 1 fixed\n")

	b.Reset()
	require.NoError(t, nlu.Write(&b, res, true))
	assert.Contains(t, b.String(), `PASS    en-GB "volume up"`+"\n")

	require.Error(t, nlu.Write(failWriter{}, res, false))
	require.Error(t, nlu.Write(failWriter{}, nil, false))
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, assert.AnError }
