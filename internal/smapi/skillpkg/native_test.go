package skillpkg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
)

func values(t *testing.T, pkg *Package, locale, typ string) map[string][]string {
	t.Helper()

	m, ok := pkg.Models[locale]
	require.True(t, ok, locale)

	out := map[string][]string{}

	for _, ty := range m.InteractionModel.LanguageModel.Types {
		if ty.Name == typ {
			for _, v := range ty.Values {
				out[v.Name.Value] = v.Name.Synonyms
			}
		}
	}

	return out
}

func nativeOptions(t *testing.T, vals map[string][]skillmodel.SlotValue, locales ...string) (*skillmodel.Sources, Options) {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	no, yes := false, true

	return src, Options{
		Locales:    locales,
		Endpoint:   Endpoint{URI: "https://encore.example.com/api/v1/alexa"},
		Privacy:    Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no},
		SlotValues: vals,
	}
}

// TestNativeSynonyms checks that the Japanese, Hindi and Arabic models carry a Latin title's native-script forms as synonyms, and the other locales don't.
func TestNativeSynonyms(t *testing.T) {
	src, opts := nativeOptions(t, map[string][]skillmodel.SlotValue{"AMAZON.Movie": {
		{ID: "movie/1", Value: "Superfly", Synonyms: []string{"super fly"}},
		{ID: "movie/2", Value: "千と千尋の神隠し"},
		{ID: "movie/3", Value: "The Beatles"},
		{ID: "movie/4", Value: "スーパーフライ"},
		{ID: "movie/5", Value: "Up", Synonyms: []string{"アップ"}},
	}}, "en-US", "ja-JP", "hi-IN", "ar-SA")

	pkg, err := Build(src, opts)
	require.NoError(t, err)
	assert.Empty(t, pkg.Warnings)

	assert.Equal(t, map[string][]string{
		"Superfly": {"super fly"}, "千と千尋の神隠し": nil, "The Beatles": nil, "スーパーフライ": nil, "Up": {"アップ"},
	}, values(t, pkg, "en-US", "AMAZON.Movie"), "English has no native forms")
	assert.Equal(t, map[string][]string{
		"Superfly": {"super fly", "スーパーフライ", "スーパー フライ"}, "千と千尋の神隠し": nil, "The Beatles": {"ザ ビートルズ", "ビートルズ"}, "スーパーフライ": nil,
		"Up": {"アップ"},
	}, values(t, pkg, "ja-JP", "AMAZON.Movie"), "the title's forms come first, then its alias's, and a form a value already has is left out")
	assert.Equal(t, []string{"super fly", "सूपरफ्लाइ", "सुपेर्फ्ल्य", "सूपर फ्लाइ", "सुपेर फ्ल्य"}, values(t, pkg, "hi-IN", "AMAZON.Movie")["Superfly"])
	assert.Equal(t, []string{"super fly", "سوبرفلاي", "سوبر فلاي"}, values(t, pkg, "ar-SA", "AMAZON.Movie")["Superfly"])
}

// TestNativeSynonymsBudget checks that native forms take only the room they are given, the likeliest form of the cheapest values first, and that a package says how many were left out.
func TestNativeSynonymsBudget(t *testing.T) {
	in := map[string][]skillmodel.SlotValue{"AMAZON.Movie": {
		{Value: "Superfly"},
		{Value: "Up"},
		{Value: "The Beatles"},
	}}

	out, added, wanted := nativeSynonyms("ja-JP", in, synonymCost("アップ")+14)
	assert.Equal(t, 1, added)
	assert.Equal(t, 4, wanted)
	assert.Equal(t, []string{"アップ"}, out["AMAZON.Movie"][1].Synonyms, "the cheapest first")
	assert.Empty(t, out["AMAZON.Movie"][0].Synonyms)
	assert.Empty(t, in["AMAZON.Movie"][1].Synonyms, "the input is left as it is")

	out, added, _ = nativeSynonyms("ja-JP", in, 1<<20)
	assert.Equal(t, 4, added)
	assert.Equal(t, []string{"ザ ビートルズ", "ビートルズ"}, out["AMAZON.Movie"][2].Synonyms)

	out, added, wanted = nativeSynonyms("en-US", in, 1<<20)
	assert.Equal(t, in, out)
	assert.Zero(t, added+wanted)

	assert.Nil(t, nativeWarning("ja-JP", 3, 3))
	assert.Equal(t, []string{"locale ja-JP: 1 of 4 native-script synonyms fit in the interaction model; titles without one are still found by Encore's matcher"}, nativeWarning("ja-JP", 1, 4))
}

// TestNativeSynonymsFitTheModel checks that a library at the slot value caps, with native forms for every title, gives a Japanese and a Hindi model no larger than WarnModelBytes, and the titles all stay.
func TestNativeSynonymsFitTheModel(t *testing.T) {
	words := strings.Fields("love story night dark star king lord return home alone fast man spider iron captain game breaking stranger walking office queen rolling stones greatest hits live summer winter blue golden heart soul fire water dream city road")
	vals := map[string][]skillmodel.SlotValue{}

	for typ, n := range map[string]int{"AMAZON.Movie": 4000, "AMAZON.TVSeries": 2000, "AMAZON.Musician": 2000, "AMAZON.MusicAlbum": 2000, "KODI_MUSIC_VIDEO": 1000} {
		for i := range n {
			title := fmt.Sprintf("%s %s %s %d", words[i%len(words)], words[(i/7)%len(words)], words[(i/3)%len(words)], i)
			vals[typ] = append(vals[typ], skillmodel.SlotValue{ID: fmt.Sprintf("x/%d", i), Value: title})
		}
	}

	src, opts := nativeOptions(t, vals, "ja-JP", "hi-IN", "en-US")

	pkg, err := Build(src, opts)
	require.NoError(t, err)

	for _, f := range pkg.Files {
		if strings.HasPrefix(f.Name, ModelDir) {
			assert.LessOrEqual(t, len(f.Data), WarnModelBytes, f.Name)
		}
	}

	assert.Len(t, values(t, pkg, "hi-IN", "AMAZON.Movie"), 4000)
	assert.Len(t, pkg.Warnings, 2, "Japanese and Hindi each leave some forms out: %v", pkg.Warnings)

	for _, w := range pkg.Warnings {
		assert.Contains(t, w, "native-script synonyms fit")
	}
}
