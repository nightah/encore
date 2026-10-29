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
	for d, want := range map[time.Duration]string{
		20 * time.Second:               "أقل من دقيقة",
		time.Minute:                    "دقيقة",
		2 * time.Minute:                "دقيقتان",
		3 * time.Minute:                "3 دقائق",
		10 * time.Minute:               "10 دقائق",
		11 * time.Minute:               "11 دقيقة",
		59 * time.Minute:               "59 دقيقة",
		time.Hour:                      "ساعة",
		2 * time.Hour:                  "ساعتان",
		61 * time.Minute:               "ساعة ودقيقة",
		122 * time.Minute:              "ساعتان ودقيقتان",
		72 * time.Minute:               "ساعة و12 دقيقة",
		3*time.Hour + 5*time.Minute:    "3 ساعات و5 دقائق",
		100 * time.Hour:                "100 ساعة",
		103 * time.Hour:                "103 ساعات",
		111*time.Hour + 30*time.Minute: "111 ساعة و30 دقيقة",
		102*time.Hour + 10*time.Minute: "102 ساعة و10 دقائق",
	} {
		assert.Equal(t, want, spokenDuration("ar", d), "%s", d)
	}
}

func TestArabicCountWithoutDual(t *testing.T) {
	assert.Equal(t, "2 أسابيع", arabicCount(2, "أسبوع", "أسابيع"), "a unit without a dual form says the number")
}

func TestSpokenListArabic(t *testing.T) {
	assert.Equal(t, "ذيب ووجدة", spokenList("ar", []string{"ذيب", "وجدة"}), "و joins an Arabic word")
	assert.Equal(t, "ذيب، وجدة وباب الحديد", spokenList("ar", []string{"ذيب", "وجدة", "باب الحديد"}))
	assert.Equal(t, "ذيب، Alien و Dune", spokenList("ar", []string{"ذيب", "Alien", "Dune"}), "و stands apart before a Latin title")
	assert.Equal(t, "Dune و 1917", spokenList("ar", []string{"Dune", "1917"}), "and before a number")
	assert.Equal(t, "Dune", spokenList("ar", []string{"Dune"}))
}

func TestRenderArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	assert.Equal(t, "يعرض الآن ذيب. المتبقي ساعة و12 دقيقة.", speech.Render("ar-SA", actions.Message{
		ID:        "playback.now_movie",
		Params:    map[string]string{"title": "ذيب"},
		Durations: map[string]time.Duration{"remaining": 72 * time.Minute},
	}))
	assert.Equal(t, "أحدث ألبوماتك: Thriller، تملي معاك والأماكن.", speech.Render("ar-SA", actions.Message{
		ID:    "whatsnew.albums",
		Lists: map[string][]string{"titles": {"Thriller", "تملي معاك", "الأماكن"}},
	}))
	assert.Equal(t, "الترجمة: الفرنسية.", speech.Render("ar-SA", actions.Message{ID: "streams.subtitles_language", Languages: map[string]string{"language": "fre"}}))
}

func TestRoomSplitsArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	for _, tc := range []struct {
		query string
		want  []RoomSplit
	}{
		{"ذيب في الصالة", []RoomSplit{{"ذيب", "الصالة"}}},
		{"وجدة في غرفة النوم", []RoomSplit{{"وجدة", "غرفة النوم"}}},
		{"صعيدي في الجامعة الأمريكية في المجلس", []RoomSplit{
			{"صعيدي", "الجامعة الأمريكية في المجلس"},
			{"صعيدي في الجامعة الأمريكية", "المجلس"},
		}},
		{"ذيب", nil},
		{"في الصالة", nil},
	} {
		assert.Equal(t, tc.want, speech.RoomSplits("ar-SA", tc.query), tc.query)
	}
}

func TestReinterpretArabic(t *testing.T) {
	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	speech, err := NewSpeech(src, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	type want struct {
		intent, title string
		slots         map[string]string
	}

	for _, tc := range []struct {
		query string
		want  want
	}{
		{"الحلقة الجاية من طاش ما طاش", want{"WatchNextEpisode", "Show", map[string]string{"Show": "طاش ما طاش"}}},
		{"الحلقة الجاية من مسلسل طاش ما طاش في الصالة", want{"WatchNextEpisode", "Show", map[string]string{"Show": "طاش ما طاش في الصالة"}}},
		{"آخر حلقة من العاصوف", want{"WatchLatestEpisode", "Show", map[string]string{"Show": "العاصوف"}}},
		{"أول حلقة من العاصوف", want{"WatchFirstEpisode", "Show", map[string]string{"Show": "العاصوف"}}},
		{"حلقة عشوائية من مسلسل جن", want{"WatchRandomEpisode", "Show", map[string]string{"Show": "جن"}}},
		{"الموسم 2 الحلقة 4 من باب الحارة", want{"WatchEpisode", "Show", map[string]string{"Show": "باب الحارة", "Season": "2", "Episode": "4"}}},
		{"طاش ما طاش", want{"PlayMedia", "Query", map[string]string{"Query": "طاش ما طاش"}}},
	} {
		in := Intent{Name: "PlayMedia", Slots: map[string]Slot{"Query": {Name: "Query", Value: tc.query}}}
		got, title := speech.Reinterpret("ar-SA", in)

		values := map[string]string{}
		for name, slot := range got.Slots {
			values[name] = slot.Value
		}

		assert.Equal(t, tc.want, want{got.Name, title, values}, tc.query)
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
