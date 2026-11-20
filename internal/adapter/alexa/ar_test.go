package alexa

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/skillmodel"
)

func TestSpokenDurationArabic(t *testing.T) {
	testCases := []struct {
		name     string
		have     time.Duration
		expected string
	}{
		{name: "ShouldSayLessThanAMinuteFor20Seconds", have: 20 * time.Second, expected: "أقل من دقيقة"},
		{name: "ShouldSayOneMinute", have: time.Minute, expected: "دقيقة"},
		{name: "ShouldUseDualForTwoMinutes", have: 2 * time.Minute, expected: "دقيقتان"},
		{name: "ShouldUsePluralForThreeMinutes", have: 3 * time.Minute, expected: "3 دقائق"},
		{name: "ShouldUsePluralForTenMinutes", have: 10 * time.Minute, expected: "10 دقائق"},
		{name: "ShouldUseSingularForElevenMinutes", have: 11 * time.Minute, expected: "11 دقيقة"},
		{name: "ShouldUseSingularFor59Minutes", have: 59 * time.Minute, expected: "59 دقيقة"},
		{name: "ShouldSayOneHour", have: time.Hour, expected: "ساعة"},
		{name: "ShouldUseDualForTwoHours", have: 2 * time.Hour, expected: "ساعتان"},
		{name: "ShouldSayOneHourAndOneMinute", have: 61 * time.Minute, expected: "ساعة ودقيقة"},
		{name: "ShouldUseDualForTwoHoursAndTwoMinutes", have: 122 * time.Minute, expected: "ساعتان ودقيقتان"},
		{name: "ShouldSayOneHourAnd12Minutes", have: 72 * time.Minute, expected: "ساعة و12 دقيقة"},
		{name: "ShouldUsePluralFor3HoursAnd5Minutes", have: 3*time.Hour + 5*time.Minute, expected: "3 ساعات و5 دقائق"},
		{name: "ShouldUseSingularFor100Hours", have: 100 * time.Hour, expected: "100 ساعة"},
		{name: "ShouldUsePluralFor103Hours", have: 103 * time.Hour, expected: "103 ساعات"},
		{name: "ShouldSay111HoursAnd30Minutes", have: 111*time.Hour + 30*time.Minute, expected: "111 ساعة و30 دقيقة"},
		{name: "ShouldSay102HoursAnd10Minutes", have: 102*time.Hour + 10*time.Minute, expected: "102 ساعة و10 دقائق"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenDuration("ar", tc.have))
		})
	}
}

func TestArabicCountWithoutDual(t *testing.T) {
	assert.Equal(t, "2 أسابيع", arabicCount(2, "أسبوع", "أسابيع"), "a unit without a dual form says the number")
}

func TestSpokenListArabic(t *testing.T) {
	testCases := []struct {
		name     string
		have     []string
		expected string
	}{
		{name: "ShouldJoinAnArabicWordWithWaw", have: []string{"ذيب", "وجدة"}, expected: "ذيب ووجدة"},
		{name: "ShouldSeparateThreeArabicTitles", have: []string{"ذيب", "وجدة", "باب الحديد"}, expected: "ذيب، وجدة وباب الحديد"},
		{name: "ShouldStandWawApartBeforeALatinTitle", have: []string{"ذيب", "Alien", "Dune"}, expected: "ذيب، Alien و Dune"},
		{name: "ShouldStandWawApartBeforeANumber", have: []string{"Dune", "1917"}, expected: "Dune و 1917"},
		{name: "ShouldSayASingleTitle", have: []string{"Dune"}, expected: "Dune"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, spokenList("ar", tc.have))
		})
	}
}

func TestRenderArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name     string
		have     actions.Message
		expected string
	}{
		{name: "ShouldSayWhatIsPlayingAndTimeRemaining", have: actions.Message{ID: "playback.now_movie", Params: map[string]string{"title": "ذيب"}, Durations: map[string]time.Duration{"remaining": 72 * time.Minute}}, expected: "يعرض الآن ذيب. المتبقي ساعة و12 دقيقة."},
		{name: "ShouldListLatestAlbums", have: actions.Message{ID: "whatsnew.albums", Lists: map[string][]string{"titles": {"Thriller", "تملي معاك", "الأماكن"}}}, expected: "أحدث ألبوماتك: Thriller، تملي معاك والأماكن."},
		{name: "ShouldNameSubtitleLanguage", have: actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "fre"}}, expected: "الترجمة: الفرنسية."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.Render("ar-SA", tc.have))
		})
	}
}

func TestRoomSplitsArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	testCases := []struct {
		name     string
		have     string
		expected []RoomSplit
	}{
		{name: "ShouldSplitRoomAfterFi", have: "ذيب في الصالة", expected: []RoomSplit{{"ذيب", "الصالة"}}},
		{name: "ShouldSplitMultiWordRoom", have: "وجدة في غرفة النوم", expected: []RoomSplit{{"وجدة", "غرفة النوم"}}},
		{name: "ShouldOfferEverySplitWhenTitleHasFi", have: "صعيدي في الجامعة الأمريكية في المجلس", expected: []RoomSplit{{"صعيدي", "الجامعة الأمريكية في المجلس"}, {"صعيدي في الجامعة الأمريكية", "المجلس"}}},
		{name: "ShouldNotSplitTitleWithoutRoom", have: "ذيب"},
		{name: "ShouldNotSplitRoomWithoutTitle", have: "في الصالة"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, speech.RoomSplits("ar-SA", tc.have))
		})
	}
}

func TestReinterpretArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	type want struct {
		intent string
		slots  map[string]string
	}

	testCases := []struct {
		name     string
		have     string
		expected want
	}{
		{name: "ShouldReadNextEpisode", have: "الحلقة الجاية من طاش ما طاش", expected: want{"WatchNextEpisode", map[string]string{"Show": "طاش ما طاش"}}},
		{name: "ShouldKeepRoomInShowAfterMusalsal", have: "الحلقة الجاية من مسلسل طاش ما طاش في الصالة", expected: want{"WatchNextEpisode", map[string]string{"Show": "طاش ما طاش في الصالة"}}},
		{name: "ShouldReadLatestEpisode", have: "آخر حلقة من العاصوف", expected: want{"WatchLatestEpisode", map[string]string{"Show": "العاصوف"}}},
		{name: "ShouldReadFirstEpisode", have: "أول حلقة من العاصوف", expected: want{"WatchFirstEpisode", map[string]string{"Show": "العاصوف"}}},
		{name: "ShouldReadRandomEpisode", have: "حلقة عشوائية من مسلسل جن", expected: want{"WatchRandomEpisode", map[string]string{"Show": "جن"}}},
		{name: "ShouldReadSeasonAndEpisode", have: "الموسم 2 الحلقة 4 من باب الحارة", expected: want{"WatchEpisode", map[string]string{"Show": "باب الحارة", "Season": "2", "Episode": "4"}}},
		{name: "ShouldKeepPlainTitleAsPlayMedia", have: "طاش ما طاش", expected: want{"PlayMedia", map[string]string{"Query": "طاش ما طاش"}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.have}}}
			actual := speech.Reinterpret("ar-SA", in)[0]

			values := map[string]string{}
			for name, slot := range actual.Slots {
				values[name] = slot.Value
			}

			assert.Equal(t, tc.expected, want{actual.Name, values})
		})
	}
}

// Arabic slot values are compared in Latin letters, which merge some letters, so no two values of a type may come to the same form under different IDs.
func TestSlotIDsArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	types, err := src.Types("ar-SA")
	require.NoError(t, err)

	for typ, values := range types {
		for _, v := range values {
			id := v.ID
			if id == "" {
				id = v.Value
			}

			for _, word := range append([]string{v.Value}, v.Synonyms...) {
				assert.Equal(t, id, speech.SlotID("ar-SA", typ, word), "%s %q", typ, word)
			}
		}
	}

	assert.Equal(t, "right", speech.SlotID("ar-SA", "NAV_ACTION", "يمين"))
	assert.Equal(t, "forward", speech.SlotID("ar-SA", "SEEK_DIRECTION", "لقدام"))
}
