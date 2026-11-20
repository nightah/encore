package livetv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// source is a media.LiveTV that counts what it is asked.
type source struct {
	mu        sync.Mutex
	channels  []media.Channel
	available bool
	err       error
	lists     int
	guides    int
	checks    int
}

func (s *source) LiveTVAvailable(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checks++

	return s.available, s.err
}

func (s *source) Channels(_ context.Context, guide bool) ([]media.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if guide {
		s.guides++
	} else {
		s.lists++
	}

	if s.err != nil {
		return nil, s.err
	}

	out := make([]media.Channel, len(s.channels))
	for i, c := range s.channels {
		if !guide {
			c.Now, c.Next = nil, nil
		}

		out[i] = c
	}

	return out, nil
}

func (s *source) PlayChannel(context.Context, int) error { return nil }

func (s *source) set(c []media.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.channels = c
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func melbourne(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	return loc
}

// misatonuc is a Melbourne Kodi instance's 184 channels from two IPTV sources. With lcn set they are numbered as Australian TV numbers them where the source playlist says how, as Kodi numbers them when it uses the sources' numbers; the rest are moved out of the way past 1000.
func misatonuc(t *testing.T, lcn bool) []media.Channel {
	t.Helper()

	data, err := os.ReadFile("testdata/channels.json")
	require.NoError(t, err)

	var rows []struct {
		ID, Number, Client, LCN int
		Name                    string
	}
	require.NoError(t, json.Unmarshal(data, &rows))

	out := make([]media.Channel, len(rows))
	for i, r := range rows {
		n := r.Number
		if lcn {
			n = r.LCN
			if n == 0 {
				n = 1000 + r.Number
			}
		}

		out[i] = media.Channel{ID: r.ID, Name: r.Name, Major: n, Client: r.Client}
	}

	return out
}

func guideFor(t *testing.T, channels []media.Channel) (*Guide, *source, *clock) {
	t.Helper()

	src := &source{channels: channels, available: true}
	clk := &clock{t: time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))}

	return New(src, "en", Options{Now: clk.now}), src, clk
}

func TestFind(t *testing.T) {
	testCases := []struct {
		name string
		lcn  bool
	}{
		{name: "ShouldFindChannelsByKodisNumbers"},
		{name: "ShouldFindChannelsByAustralianNumbers", lcn: true},
	}

	spokenCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldFindSeven", have: "seven", expected: "Seven"},
		{name: "ShouldFindSevenCapitalised", have: "Seven", expected: "Seven"},
		{name: "ShouldFindNineGo", have: "nine go", expected: "9Go!"},
		{name: "ShouldFindNineGem", have: "nine gem", expected: "9Gem"},
		{name: "ShouldFindSevenTwoSpoken", have: "seven two", expected: "7two"},
		{name: "ShouldFindSevenTwoInDigits", have: "72", expected: "7two"},
		{name: "ShouldFindSevenMate", have: "seven mate", expected: "7mate"},
		{name: "ShouldFindTen", have: "ten", expected: "10"},
		{name: "ShouldFindTenDrama", have: "ten drama", expected: "10 Drama"},
		{name: "ShouldFindABCNews", have: "abc news", expected: "ABC NEWS"},
		{name: "ShouldFindABCKidsSpelledOut", have: "a b c kids", expected: "ABC Kids"},
		{name: "ShouldFindSBS", have: "sbs", expected: "SBS"},
		{name: "ShouldFindSBSRadioOne", have: "sbs radio one", expected: "SBS Radio 1"},
		{name: "ShouldFindFoxSports503Spoken", have: "fox sports five oh three", expected: "Fox Sports 503"},
		{name: "ShouldFindFoxSports503InDigits", have: "fox sports 503", expected: "Fox Sports 503"},
		{name: "ShouldFindChannelNine", have: "channel nine", expected: "Channel 9"},
		{name: "ShouldFindChannelNineByNumber", have: "nine", expected: "Channel 9"},
		{name: "ShouldFindChannelThirtyOne", have: "thirty one", expected: "Channel 31"},
		{name: "ShouldFindESPNTwo", have: "espn two", expected: "ESPN2"},
		{name: "ShouldFindFoxFooty", have: "fox footy", expected: "Fox Footy"},
		{name: "ShouldFindAlJazeera", have: "al jazeera", expected: "Al Jazeera"},
		{name: "ShouldFindSeinfeld", have: "seinfeld", expected: "Seinfeld"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g, _, _ := guideFor(t, misatonuc(t, tc.lcn))

			for _, sc := range spokenCases {
				t.Run(sc.name, func(t *testing.T) {
					found, err := g.Find(t.Context(), sc.have)
					require.NoError(t, err)
					assert.Equal(t, match.DecisionPlay, found.Decision)
					assert.Equal(t, sc.expected, found.Channel.Name)
				})
			}
		})
	}
}

func TestFindByNumberWhenNoName(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))

	found, err := g.Find(t.Context(), "twenty four")
	require.NoError(t, err)
	assert.Equal(t, match.DecisionPlay, found.Decision)
	assert.Equal(t, "Air Crash Investigations", found.Channel.Name, "Kodi numbers it 24")
	assert.Nil(t, found.Result, "a number decided it")

	g, _, _ = guideFor(t, misatonuc(t, true))

	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldFindTwentyFourAsABCNews", have: "twenty four", expected: "ABC NEWS"},
		{name: "ShouldFindSeventyTwoAsSevenTwo", have: "seventy two", expected: "7two"},
		{name: "ShouldFindTwentyOneAsABCTV", have: "twenty one", expected: "ABC TV"},
		{name: "ShouldFindNinetyOneAsChannelNine", have: "ninety one", expected: "Channel 9"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := g.Find(t.Context(), tc.have)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, found.Channel.Name)
		})
	}
}

func TestFindAsks(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))

	found, err := g.Find(t.Context(), "abc")
	require.NoError(t, err)
	require.Equal(t, match.DecisionAsk, found.Decision)
	require.NotNil(t, found.Result)

	names := make([]string, len(found.Choices))
	for i, c := range found.Choices {
		names[i] = c.Name
	}

	assert.Equal(t, "ABC TV", names[0])
	assert.Subset(t, names, []string{"ABC Kids", "ABC NEWS"})
}

func TestFindNotFound(t *testing.T) {
	g, src, _ := guideFor(t, misatonuc(t, false))

	found, err := g.Find(t.Context(), "zzz qqq")
	require.NoError(t, err)
	assert.Equal(t, match.DecisionNotFound, found.Decision)
	assert.Equal(t, 1, src.lists, "a list just fetched isn't fetched again for a miss")
}

func TestFindDuplicates(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))

	testCases := []struct {
		name     string
		have     string
		expected int
	}{
		{name: "ShouldPickTheLowestNumberedBBCNews", have: "bbc news", expected: 73},
		{name: "ShouldPickTheLowestNumberedJudgeJudy", have: "judge judy", expected: 43},
		{name: "ShouldPickTheLowestNumberedRacingDotCom", have: "racing dot com", expected: 17},
		{name: "ShouldPickTheLowestNumberedGhostHunters", have: "ghost hunters", expected: 29},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := g.Find(t.Context(), tc.have)
			require.NoError(t, err)
			assert.Equal(t, match.DecisionPlay, found.Decision)
			assert.Equal(t, tc.expected, found.Channel.Major, "the lowest numbered of the channels with the name")
		})
	}
}

func TestPickTiedNumber(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	on := &media.Broadcast{Title: "FOX SPORTS News", Start: now.Add(-time.Minute), End: now.Add(time.Hour)}
	g, src, _ := guideFor(t, []media.Channel{
		{ID: 3159, Name: "DW English", Major: 173, Client: 5},
		{ID: 3164, Name: "Fox Sports News", Major: 173, Client: 6, Now: on},
	})

	c, ok, err := g.ByNumber(t.Context(), 173, -1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Fox Sports News", c.Name, "of two channels numbered 173, the one with something on now")
	assert.Equal(t, 1, src.guides)

	src.set([]media.Channel{{ID: 3164, Name: "Fox Sports News", Major: 173, Client: 6}, {ID: 3159, Name: "DW English", Major: 173, Client: 5}})
	g.Forget()

	c, _, err = g.ByNumber(t.Context(), 173, -1)
	require.NoError(t, err)
	assert.Equal(t, "DW English", c.Name, "with nothing on either, the lowest ID")
}

func TestByNumberSubChannel(t *testing.T) {
	g, _, _ := guideFor(t, []media.Channel{
		{ID: 1, Name: "SBS", Major: 3},
		{ID: 2, Name: "SBS HD", Major: 3, SubNumber: 1},
		{ID: 3, Name: "SBS Viceland", Major: 3, SubNumber: 2},
	})

	testCases := []struct {
		name       string
		major      int
		sub        int
		expected   string
		expectedOK bool
	}{
		{name: "ShouldFindTheMainChannelWithoutASubNumber", major: 3, sub: -1, expected: "SBS", expectedOK: true},
		{name: "ShouldFindTheSubChannel", major: 3, sub: 2, expected: "SBS Viceland", expectedOK: true},
		{name: "ShouldFindTheMainChannelForSubNumberZero", major: 3, expected: "SBS", expectedOK: true},
		{name: "ShouldNotFindAMissingSubChannel", major: 3, sub: 5},
		{name: "ShouldNotFindAMissingNumber", major: 4, sub: -1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok, err := g.ByNumber(t.Context(), tc.major, tc.sub)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedOK, ok)
			assert.Equal(t, tc.expected, c.Name)
		})
	}

	found, err := g.Find(t.Context(), "three two")
	require.NoError(t, err)
	assert.Equal(t, "SBS Viceland", found.Channel.Name, "32 isn't a channel, so 3.2")
}

func TestChannelsCached(t *testing.T) {
	g, src, clk := guideFor(t, misatonuc(t, false))

	_, err := g.Find(t.Context(), "seven")
	require.NoError(t, err)
	_, err = g.Find(t.Context(), "sbs")
	require.NoError(t, err)
	assert.Equal(t, 1, src.lists)

	clk.t = clk.t.Add(2 * time.Minute)
	src.set(append(misatonuc(t, false), media.Channel{ID: 5000, Name: "Gardening Australia 24/7", Major: 500}))

	found, err := g.Find(t.Context(), "gardening australia twenty four seven")
	require.NoError(t, err)
	assert.Equal(t, "Gardening Australia 24/7", found.Channel.Name, "a name nothing matched fetches the list again")
	assert.Equal(t, 2, src.lists)

	clk.t = clk.t.Add(channelTTL)
	_, err = g.Channels(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, src.lists, "the list expires")
}

func TestNoChannels(t *testing.T) {
	g, _, _ := guideFor(t, nil)

	_, err := g.Find(t.Context(), "seven")
	require.ErrorIs(t, err, ErrNoChannels)
	assert.True(t, IsNoLiveTV(err))
}

func TestAvailable(t *testing.T) {
	g, src, clk := guideFor(t, nil)
	src.available = false

	ok, err := g.Available(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)

	src.available = true
	ok, _ = g.Available(t.Context())
	assert.False(t, ok, "kept a little while")

	clk.t = clk.t.Add(availableRecheck)
	ok, _ = g.Available(t.Context())
	assert.True(t, ok, "asked again soon, as the TV service may have been starting")

	src.available = false
	clk.t = clk.t.Add(availableRecheck)
	ok, _ = g.Available(t.Context())
	assert.True(t, ok, "live TV, once there, is believed for longer")
	assert.Equal(t, 2, src.checks)
}

func TestOnNowCache(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	g, src, clk := guideFor(t, []media.Channel{
		{ID: 1, Name: "Seven", Major: 71, Now: &media.Broadcast{Title: "House Of Wellness", Start: now.Add(-10 * time.Minute), End: now.Add(20 * time.Second)}},
	})

	_, err := g.OnNow(t.Context())
	require.NoError(t, err)

	clk.t = now.Add(10 * time.Second)
	_, _ = g.OnNow(t.Context())
	assert.Equal(t, 1, src.guides)

	clk.t = now.Add(21 * time.Second)
	_, _ = g.OnNow(t.Context())
	assert.Equal(t, 2, src.guides, "dropped when the programme on now ends")

	clk.t = clk.t.Add(guideTTL)
	_, _ = g.OnNow(t.Context())
	assert.Equal(t, 3, src.guides, "and within a minute anyway")
}

func TestFindProgramme(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	slot := func(title string, from, to time.Duration) *media.Broadcast {
		return &media.Broadcast{Title: title, Start: now.Add(from), End: now.Add(to)}
	}
	g, _, _ := guideFor(t, []media.Channel{
		{ID: 1, Name: "Seven", Major: 71, Now: slot("House Of Wellness", -10*time.Minute, 20*time.Minute), Next: slot("The Amazing Race", 20*time.Minute, 80*time.Minute)},
		{ID: 2, Name: "ABC TV", Major: 21, Now: slot("Austin", -5*time.Minute, 25*time.Minute), Next: slot("Gardening Australia", 25*time.Minute, 55*time.Minute)},
		{ID: 3, Name: "Fox Cricket", Major: 501, Now: slot("Test Cricket", -time.Hour, time.Hour)},
		{ID: 4, Name: "Kayo Cricket", Major: 401, Now: slot("Test Cricket", -time.Hour, time.Hour)},
		{ID: 5, Name: "7mate", Major: 74, Next: slot("Austin", 30*time.Minute, time.Hour)},
	})

	testCases := []struct {
		name          string
		have          string
		expected      string
		expectedOK    bool
		expectedNow   bool
		expectedStart time.Time
	}{
		{name: "ShouldFindAProgrammeOnNow", have: "house of wellness", expected: "Seven", expectedOK: true, expectedNow: true},
		{name: "ShouldFindAProgrammeOnNext", have: "gardening australia", expected: "ABC TV", expectedOK: true, expectedStart: now.Add(25 * time.Minute)},
		{name: "ShouldPickTheLowestNumberedOfTwoChannels", have: "test cricket", expected: "Kayo Cricket", expectedOK: true, expectedNow: true},
		{name: "ShouldPreferOnNowOverOnNextThoughTheNumberIsHigher", have: "austin", expected: "ABC TV", expectedOK: true, expectedNow: true},
		{name: "ShouldNotFindAProgrammeNotOn", have: "neighbours"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok, err := g.FindProgramme(t.Context(), tc.have)
			require.NoError(t, err)
			require.Equal(t, tc.expectedOK, ok)

			if !tc.expectedOK {
				return
			}

			assert.Equal(t, tc.expected, p.Channel.Name)
			assert.Equal(t, tc.expectedNow, p.OnNow)

			if !tc.expectedStart.IsZero() {
				assert.Equal(t, tc.expectedStart, p.Broadcast.Start)
			}
		})
	}
}

func TestNames(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))

	names, err := g.Names(t.Context())
	require.NoError(t, err)
	assert.Contains(t, names, "9Go!")
	assert.Len(t, names, 178, "184 channels, 6 names twice")
}

func TestJoinDigits(t *testing.T) {
	testCases := []struct {
		name     string
		locale   string
		have     string
		expected string
	}{
		{name: "ShouldJoinDigitsWithOhAsZero", locale: "en", have: "fox sports 5 oh 3", expected: "fox sports 503"},
		{name: "ShouldJoinTwoDigits", locale: "en", have: "7 2", expected: "72"},
		{name: "ShouldKeepANumber", locale: "en", have: "24", expected: "24"},
		{name: "ShouldKeepTwoDigitNumbersApart", locale: "en", have: "24 7", expected: "24 7"},
		{name: "ShouldKeepADigitAfterAWord", locale: "en", have: "sbs radio 1", expected: "sbs radio 1"},
		{name: "ShouldKeepALeadingOh", locale: "en", have: "oh 3", expected: "oh 3"},
		{name: "ShouldJoinDigitsWithOAsZero", locale: "en", have: "1 o 1", expected: "101"},
		{name: "ShouldReadOhAsZeroOnlyInEnglish", locale: "fr", have: "5 oh 3", expected: "5 oh 3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, joinDigits(tc.locale, tc.have))
		})
	}
}

func TestAliases(t *testing.T) {
	testCases := []struct {
		name     string
		locale   string
		have     string
		expected []string
	}{
		{name: "ShouldSplitADigitFromAWord", locale: "en", have: "9Go!", expected: []string{"9 Go!"}},
		{name: "ShouldSplitAWordFromADigit", locale: "en", have: "ESPN2", expected: []string{"ESPN 2"}},
		{name: "ShouldSplitADigitFromALowercaseWord", locale: "en", have: "7two", expected: []string{"7 two"}},
		{name: "ShouldHaveNoAliasForPlainWords", locale: "en", have: "ABC NEWS"},
		{name: "ShouldHaveNoAliasForANumberAlreadyApart", locale: "en", have: "Fox Sports 503"},
		{name: "ShouldSpeakADot", locale: "en", have: "Racing.com", expected: []string{"Racing dot com"}},
		{name: "ShouldNotSpeakATrailingDot", locale: "en", have: "GOOD."},
		{name: "ShouldSpeakAOneAfterAWord", locale: "fr-FR", have: "TF1", expected: []string{"TF 1", "TF un"}},
		{name: "ShouldSpeakAOneInDutch", locale: "nl", have: "NPO 1", expected: []string{"NPO een", "NPO één"}},
		{name: "ShouldSpeakOnlyAOneOnItsOwn", locale: "fr", have: "Canal 10"},
		{name: "ShouldNotSpeakAOneTheNameStartsWith", locale: "fr", have: "1Plus", expected: []string{"1 Plus"}},
		{name: "ShouldNotSpeakAOneInEnglish", locale: "en", have: "ESPN1", expected: []string{"ESPN 1"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Aliases(tc.locale, tc.have))
		})
	}
}

func TestChannelNow(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	g, _, _ := guideFor(t, []media.Channel{
		{ID: 24, Name: "ABC NEWS", Major: 24, Now: &media.Broadcast{Title: "The World", Start: now.Add(-time.Minute), End: now.Add(time.Hour)}},
	})

	testCases := []struct {
		name       string
		have       int
		expected   string
		expectedOK bool
	}{
		{name: "ShouldSayWhatIsOnNow", have: 24, expected: "The World", expectedOK: true},
		{name: "ShouldNotFindAMissingNumber", have: 99},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok, err := g.ChannelNow(t.Context(), tc.have)
			require.NoError(t, err)
			require.Equal(t, tc.expectedOK, ok)

			if tc.expectedOK {
				assert.Equal(t, tc.expected, c.Now.Title)
			}
		})
	}
}

func TestSourceErrors(t *testing.T) {
	broken := errors.New("kodi went away")
	g, src, _ := guideFor(t, misatonuc(t, false))
	src.err = broken

	_, err := g.Find(t.Context(), "seven")
	require.ErrorIs(t, err, broken)
	_, _, err = g.ByNumber(t.Context(), 7, -1)
	require.ErrorIs(t, err, broken)
	_, err = g.Names(t.Context())
	require.ErrorIs(t, err, broken)
	_, _, err = g.ChannelNow(t.Context(), 1)
	require.ErrorIs(t, err, broken)
	_, _, err = g.FindProgramme(t.Context(), "austin")
	require.ErrorIs(t, err, broken)
	_, err = g.Available(t.Context())
	require.ErrorIs(t, err, broken)
	assert.False(t, IsNoLiveTV(err))
	assert.True(t, IsNoLiveTV(media.ErrNoLiveTV))
}

func TestNewDefaults(t *testing.T) {
	g := New(&source{}, "fr", Options{})
	assert.Equal(t, "fr", g.Language())
	assert.WithinDuration(t, time.Now(), g.now(), time.Minute, "the wall clock")
	assert.Equal(t, match.RetrieverTrigram, g.match.Retriever, "trigrams, for a list this small")

	_, ok, err := g.FindProgramme(t.Context(), "anything")
	require.NoError(t, err)
	assert.False(t, ok, "a guide with nothing on finds nothing")
}

func TestFindSpokenOne(t *testing.T) {
	testCases := []struct {
		name          string
		locale        string
		channels      []media.Channel
		have          string
		expected      string
		notOneChannel string
	}{
		{name: "ShouldFindASpokenOneInFR", locale: "fr", channels: []media.Channel{{ID: 1, Name: "TF1", Major: 1}, {ID: 2, Name: "France 2", Major: 2}, {ID: 3, Name: "M6", Major: 6}}, have: "tf un", expected: "TF1", notOneChannel: "france un"},
		{name: "ShouldFindASpokenOneInNL", locale: "nl", channels: []media.Channel{{ID: 1, Name: "NPO 1", Major: 1}, {ID: 2, Name: "NPO 2", Major: 2}, {ID: 3, Name: "RTL 4", Major: 4}}, have: "npo een", expected: "NPO 1", notOneChannel: "rtl een"},
		{name: "ShouldFindASpokenOneInIT", locale: "it", channels: []media.Channel{{ID: 1, Name: "Rai 1", Major: 1}, {ID: 2, Name: "Rai 2", Major: 2}}, have: "rai uno", expected: "Rai 1"},
		{name: "ShouldFindASpokenOneInPT", locale: "pt", channels: []media.Channel{{ID: 1, Name: "SIC", Major: 3}, {ID: 2, Name: "RTP1", Major: 1}}, have: "rtp um", expected: "RTP1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := &source{channels: tc.channels, available: true}
			g := New(src, tc.locale, Options{Now: (&clock{t: time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))}).now})

			found, err := g.Find(t.Context(), tc.have)
			require.NoError(t, err)
			assert.Equal(t, match.DecisionPlay, found.Decision, "Alexa writes a 1 after a name as the word, which %s also uses for “a”", tc.locale)
			assert.Equal(t, tc.expected, found.Channel.Name)

			if tc.notOneChannel != "" {
				found, err = g.Find(t.Context(), tc.notOneChannel)
				require.NoError(t, err)
				assert.NotEqual(t, match.DecisionPlay, found.Decision, "only a channel with a 1 in its name is one")
			}
		})
	}
}

func TestFindWhatIsNoChannel(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, true))

	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldFindNothingWhenNothingIsSaid"},
		{name: "ShouldFindNothingForOnlyPunctuation", have: "?"},
		{name: "ShouldFindNothingForANumberNoChannelHasTooShortToBeASubChannel", have: "5"},
		{name: "ShouldFindNothingForANumberTooLongToBeAChannels", have: "99999999999999999999"},
		{name: "ShouldFindNothingLikeItWithItsDigitsApartOrJoined", have: "zzz 4 5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := g.Find(t.Context(), tc.have)
			require.NoError(t, err)
			assert.Equal(t, match.DecisionNotFound, found.Decision)
		})
	}
}

func TestFindKeepsTheDigitsApartWhenThatMatchesBetter(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))

	found, err := g.Find(t.Context(), "sbs 3 1")
	require.NoError(t, err)
	require.Equal(t, match.DecisionAsk, found.Decision)
	assert.Equal(t, "sbs 3 1", found.Result.Normalized, "SBS 31 is no channel's name")
	assert.Equal(t, "SBS", found.Choices[0].Name)
}

func TestFindStopsWhenCancelled(t *testing.T) {
	g, _, _ := guideFor(t, misatonuc(t, false))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := g.Find(ctx, "abc news")
	require.ErrorIs(t, err, context.Canceled)
}

func TestFindAsksAboutANameOnce(t *testing.T) {
	g, _, _ := guideFor(t, []media.Channel{
		{ID: 1, Name: "Sky News", Major: 51},
		{ID: 2, Name: "Sky-News", Major: 52},
		{ID: 3, Name: "Sky Sports", Major: 53},
	})

	found, err := g.Find(t.Context(), "sky")
	require.NoError(t, err)
	require.Equal(t, match.DecisionAsk, found.Decision)

	names := make([]string, len(found.Choices))
	for i, c := range found.Choices {
		names[i] = c.Name
	}

	assert.ElementsMatch(t, []string{"Sky News", "Sky Sports"}, names, "Sky News and Sky-News are said the same, so the lower numbered stands for both")
}

func TestPickPrefersOnNowWhicheverComesFirst(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	on := &media.Broadcast{Title: "FOX SPORTS News", Start: now.Add(-time.Minute), End: now.Add(time.Hour)}

	testCases := []struct {
		name string
		have []media.Channel
	}{
		{name: "ShouldPickOnNowListedSecond", have: []media.Channel{{ID: 3159, Name: "DW English", Major: 173}, {ID: 3164, Name: "Fox Sports News", Major: 173, Now: on}}},
		{name: "ShouldPickOnNowListedFirst", have: []media.Channel{{ID: 3164, Name: "Fox Sports News", Major: 173, Now: on}, {ID: 3159, Name: "DW English", Major: 173}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g, _, _ := guideFor(t, tc.have)

			c, ok, err := g.ByNumber(t.Context(), 173, -1)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "Fox Sports News", c.Name)
		})
	}
}

func TestFindProgrammeAsked(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	slot := func(title string) *media.Broadcast {
		return &media.Broadcast{Title: title, Start: now.Add(-time.Hour), End: now.Add(time.Hour)}
	}
	g, _, _ := guideFor(t, []media.Channel{
		{ID: 1, Name: "Fox Cricket", Major: 501, Now: slot("Cricket Live")},
		{ID: 2, Name: "Kayo Cricket", Major: 401, Now: slot("Cricket Gold")},
		{ID: 3, Name: "Fox Footy", Major: 504, Now: slot("AFL: Live")},
		{ID: 4, Name: "Kayo Footy", Major: 404, Now: slot("AFL Live")},
	})

	testCases := []struct {
		name       string
		have       string
		expected   string
		expectedOK bool
	}{
		// Two programmes might be meant, and a channel's name may yet be.
		{name: "ShouldNotPickWhenTwoProgrammesMightBeMeant", have: "cricket"},
		// One programme, written two ways: the lower numbered of the channels showing it.
		{name: "ShouldPickTheLowerNumberedChannelForOneProgrammeWrittenTwoWays", have: "afl", expected: "Kayo Footy", expectedOK: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok, err := g.FindProgramme(t.Context(), tc.have)
			require.NoError(t, err)
			require.Equal(t, tc.expectedOK, ok)

			if tc.expectedOK {
				assert.Equal(t, tc.expected, p.Channel.Name)
			}
		})
	}
}

func TestOnNowKeptAFewSeconds(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	g, src, clk := guideFor(t, []media.Channel{
		{ID: 1, Name: "Seven", Major: 71, Now: &media.Broadcast{Title: "House Of Wellness", Start: now.Add(-10 * time.Minute), End: now.Add(time.Second)}},
	})

	_, err := g.OnNow(t.Context())
	require.NoError(t, err)

	clk.t = now.Add(guideMin - time.Second)
	_, _ = g.OnNow(t.Context())
	assert.Equal(t, 1, src.guides, "a programme ending in a second doesn't send every request to Kodi")

	clk.t = now.Add(guideMin)
	_, _ = g.OnNow(t.Context())
	assert.Equal(t, 2, src.guides)
}

func TestChannelsWaitsForAFetchUnderWay(t *testing.T) {
	g, src, clk := guideFor(t, misatonuc(t, false))

	// The clock is read first when a request looks for the list, so the request has found none once it is.
	looked := make(chan struct{}, 1)
	g.now = func() time.Time {
		select {
		case looked <- struct{}{}:
		default:
		}

		return clk.t
	}

	g.load.Lock() // another request is fetching the list

	got := make(chan error, 1)

	go func() {
		_, err := g.Channels(t.Context())
		got <- err
	}()

	<-looked

	channels, err := src.Channels(t.Context(), false)
	require.NoError(t, err)
	ix, err := g.build(channels)
	require.NoError(t, err)

	g.mu.Lock()
	g.channels, g.index, g.loaded = channels, ix, clk.t
	g.mu.Unlock()
	g.load.Unlock()

	require.NoError(t, <-got)
	assert.Equal(t, 1, src.lists, "the waiting request takes the list the other fetched")
}

func TestUnnamedChannelsAreNotMatched(t *testing.T) {
	g, _, _ := guideFor(t, []media.Channel{{ID: 1, Name: "", Major: 1}, {ID: 2, Name: "Seven", Major: 71}})

	found, err := g.Find(t.Context(), "seven")
	require.NoError(t, err)
	assert.Equal(t, "Seven", found.Channel.Name)

	names, err := g.Names(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"Seven"}, names)
}

func TestMatcherRefused(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 26, 0, 0, melbourne(t))
	src := &source{available: true, channels: []media.Channel{
		{ID: 1, Name: "ABC TV", Major: 21, Now: &media.Broadcast{Title: "Austin", Start: now.Add(-5 * time.Minute), End: now.Add(25 * time.Minute)}},
	}}
	g := New(src, "en", Options{Now: (&clock{t: now}).now, Match: match.Config{Retriever: "nope"}})

	_, err := g.Find(t.Context(), "abc tv")
	require.ErrorContains(t, err, `unknown retriever "nope"`)
	_, _, err = g.FindProgramme(t.Context(), "austin")
	require.ErrorContains(t, err, `unknown retriever "nope"`)

	// Refused, nothing was kept: the next request fetches again.
	_, err = g.Find(t.Context(), "abc tv")
	require.Error(t, err)
	assert.Equal(t, 2, src.lists)
}

func TestNewIndexRefusesADocWithoutID(t *testing.T) {
	g, _, _ := guideFor(t, nil)

	_, err := g.newIndex([]match.Doc{{Kind: media.KindChannel, Title: "ABC TV"}})
	require.ErrorContains(t, err, "doc without ID")
}
