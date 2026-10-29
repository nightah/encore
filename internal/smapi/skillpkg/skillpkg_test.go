package skillpkg_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

func sources(t *testing.T) *skillmodel.Sources {
	t.Helper()

	src, err := skillmodel.Load(i18n.FS)
	require.NoError(t, err)

	return src
}

func answered() skillpkg.Privacy {
	no, yes := false, true

	return skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no}
}

func options() skillpkg.Options {
	return skillpkg.Options{
		Locales:  []string{"en-US", "en-GB"},
		Endpoint: skillpkg.Endpoint{URI: "https://encore.example.com/api/v1/alexa"},
		Privacy:  answered(),
	}
}

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	out := map[string][]byte{}

	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)

		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		out[f.Name] = b
	}

	return out
}

func TestBuild(t *testing.T) {
	pkg, err := skillpkg.Build(sources(t), options())
	require.NoError(t, err)

	names := make([]string, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		names = append(names, f.Name)
	}

	assert.Equal(t, []string{
		"skill-package/assets/images/large-icon.png",
		"skill-package/assets/images/small-icon.png",
		"skill-package/interactionModels/custom/en-GB.json",
		"skill-package/interactionModels/custom/en-US.json",
		"skill-package/skill.json",
	}, names)
	assert.Equal(t, []string{"en-GB", "en-US"}, pkg.Locales())
	assert.Equal(t, "encore", pkg.Invocation("en-GB"))
	assert.Empty(t, pkg.Invocation("fr-FR"))

	data, err := pkg.Zip()
	require.NoError(t, err)

	files := unzip(t, data)

	var m map[string]any
	require.NoError(t, json.Unmarshal(files[skillpkg.ManifestPath], &m))

	manifest := m["manifest"].(map[string]any)
	assert.Equal(t, "1.0", manifest["manifestVersion"])

	pub := manifest["publishingInformation"].(map[string]any)
	assert.Equal(t, "SMART_HOME", pub["category"])
	assert.Equal(t, false, pub["isAvailableWorldwide"])
	assert.Equal(t, []any{"GB", "US"}, pub["distributionCountries"])

	us := pub["locales"].(map[string]any)["en-US"].(map[string]any)
	assert.Equal(t, "Encore", us["name"])
	assert.Contains(t, us["examplePhrases"], "Alexa, open encore")
	assert.LessOrEqual(t, len(us["summary"].(string)), 160)

	// Answers the user gave as false are sent, not omitted.
	privacy := manifest["privacyAndCompliance"].(map[string]any)
	assert.Equal(t, map[string]any{
		"allowsPurchases": false, "usesPersonalInfo": false, "isChildDirected": false, "isExportCompliant": true, "containsAds": false,
	}, privacy)

	custom := manifest["apis"].(map[string]any)["custom"].(map[string]any)
	assert.Equal(t, map[string]any{"uri": "https://encore.example.com/api/v1/alexa", "sslCertificateType": "Wildcard"}, custom["endpoint"])
	assert.Equal(t, []any{}, custom["interfaces"])

	var model skillmodel.InteractionModel
	require.NoError(t, json.Unmarshal(files["skill-package/interactionModels/custom/en-GB.json"], &model))
	assert.Equal(t, "encore", model.InteractionModel.LanguageModel.InvocationName)
}

func TestBuildOverrides(t *testing.T) {
	opts := options()
	opts.Invocation = "my kodi"
	opts.SkillName = "My Kodi"
	opts.Category = "STREAMING_SERVICE"

	pkg, err := skillpkg.Build(sources(t), opts)
	require.NoError(t, err)

	pub := pkg.Manifest.Manifest.PublishingInformation
	assert.Equal(t, "STREAMING_SERVICE", pub.Category)
	assert.Equal(t, "My Kodi", pub.Locales["en-US"].Name)
	assert.Equal(t, "my kodi", pkg.Invocation("en-US"))
	assert.Contains(t, pub.Locales["en-US"].ExamplePhrases, "Alexa, open my kodi")
}

func TestBuildRegionalLocales(t *testing.T) {
	opts := options()
	opts.Locales = []string{"fr-CA", "es-US", "es-MX"}

	pkg, err := skillpkg.Build(sources(t), opts)
	require.NoError(t, err)

	assert.Equal(t, []string{"es-MX", "es-US", "fr-CA"}, pkg.Locales())
	assert.Equal(t, "encore", pkg.Invocation("es-MX"))
	assert.Equal(t, "encore", pkg.Invocation("es-US"))
	assert.Equal(t, "kodi", pkg.Invocation("fr-CA"), "encore means again in Canadian French too")
	assert.Equal(t, []string{"CA", "MX", "US"}, pkg.Manifest.Manifest.PublishingInformation.DistributionCountries)

	opts.Invocation = skillmodel.InvocationKodi

	pkg, err = skillpkg.Build(sources(t), opts)
	require.NoError(t, err)

	for _, locale := range pkg.Locales() {
		assert.Equal(t, "kodi", pkg.Invocation(locale), locale)
	}
}

func TestDeterministic(t *testing.T) {
	src := sources(t)

	a, err := skillpkg.Build(src, options())
	require.NoError(t, err)

	opts := options()
	opts.Locales = []string{"en-GB", "en-US", "en-GB"}

	b, err := skillpkg.Build(src, opts)
	require.NoError(t, err)

	assert.Equal(t, a.Hash(), b.Hash())
	assert.Len(t, a.Hash(), 64)

	za, err := a.Zip()
	require.NoError(t, err)

	zb, err := b.Zip()
	require.NoError(t, err)

	assert.Equal(t, za, zb)

	opts.SlotValues = map[string][]skillmodel.SlotValue{"AMAZON.Movie": {{Value: "Heat"}}}

	c, err := skillpkg.Build(src, opts)
	require.NoError(t, err)
	assert.NotEqual(t, a.Hash(), c.Hash())
}

func TestPrivacyRequired(t *testing.T) {
	opts := options()
	opts.Privacy = skillpkg.Privacy{}

	_, err := skillpkg.Build(sources(t), opts)
	require.ErrorIs(t, err, skillpkg.ErrPrivacyUnanswered)
	assert.Contains(t, err.Error(), "allowsPurchases, containsAds, isChildDirected, isExportCompliant, usesPersonalInfo")

	opts.Privacy = answered()
	opts.Privacy.ContainsAds = nil

	_, err = skillpkg.Build(sources(t), opts)
	require.ErrorIs(t, err, skillpkg.ErrPrivacyUnanswered)
	assert.Contains(t, err.Error(), "containsAds")
}

func TestInvalidOptions(t *testing.T) {
	for name, mutate := range map[string]func(*skillpkg.Options){
		"no locales":     func(o *skillpkg.Options) { o.Locales = nil },
		"unknown locale": func(o *skillpkg.Options) { o.Locales = []string{"xx-XX"} },
		"http endpoint":  func(o *skillpkg.Options) { o.Endpoint.URI = "http://encore.example.com/api/v1/alexa" },
		"no endpoint":    func(o *skillpkg.Options) { o.Endpoint.URI = "" },
		"short name":     func(o *skillpkg.Options) { o.SkillName = "E" },
	} {
		t.Run(name, func(t *testing.T) {
			opts := options()
			mutate(&opts)

			_, err := skillpkg.Build(sources(t), opts)
			require.Error(t, err)
		})
	}
}

func TestLongSlotValuesDropped(t *testing.T) {
	opts := options()
	long := strings.Repeat("x", skillpkg.MaxSlotValueLength+1)
	opts.SlotValues = map[string][]skillmodel.SlotValue{"AMAZON.Movie": {
		{Value: "Heat", Synonyms: []string{"heat 1995", long}},
		{Value: long},
		{Value: strings.Repeat("é", skillpkg.MaxSlotValueLength)},
	}}

	pkg, err := skillpkg.Build(sources(t), opts)
	require.NoError(t, err)
	require.Len(t, pkg.Warnings, 1)
	assert.Contains(t, pkg.Warnings[0], "dropped 2 values or synonyms")

	var movie *skillmodel.ModelType

	for i, typ := range pkg.Models["en-US"].InteractionModel.LanguageModel.Types {
		if typ.Name == "AMAZON.Movie" {
			movie = &pkg.Models["en-US"].InteractionModel.LanguageModel.Types[i]
		}
	}

	require.NotNil(t, movie)
	require.Len(t, movie.Values, 2)
	assert.Equal(t, []string{"heat 1995"}, movie.Values[0].Name.Synonyms)
}

func movies(n, length int) []skillmodel.SlotValue {
	out := make([]skillmodel.SlotValue, n)
	for i := range out {
		out[i].Value = fmt.Sprintf("%0*d", length, i)
	}

	return out
}

func TestModelSizeLimits(t *testing.T) {
	src := sources(t)

	opts := options()
	opts.Locales = []string{"en-US"}
	opts.SlotValues = map[string][]skillmodel.SlotValue{"AMAZON.Movie": movies(10_000, 100)}

	pkg, err := skillpkg.Build(src, opts)
	require.NoError(t, err)
	require.Len(t, pkg.Warnings, 1)
	assert.Contains(t, pkg.Warnings[0], "close to Amazon's")

	opts.SlotValues = map[string][]skillmodel.SlotValue{"AMAZON.Movie": movies(15_000, 100)}

	_, err = skillpkg.Build(src, opts)
	require.ErrorContains(t, err, "over Amazon's 1500000 byte limit")
}

func TestExport(t *testing.T) {
	pkg, err := skillpkg.Build(sources(t), options())
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, pkg.Export(&buf))

	files := unzip(t, buf.Bytes())
	assert.Contains(t, files, skillpkg.ManifestPath)
	assert.Contains(t, files, "skill-package/interactionModels/custom/en-US.json")
	require.Contains(t, files, "ask-resources.json")

	var res map[string]any
	require.NoError(t, json.Unmarshal(files["ask-resources.json"], &res))
	assert.Equal(t, "2020-03-31", res["askcliResourcesVersion"])
}

// The manifest points every locale at the bundled icons, which are the sizes Amazon requires.
func TestIcons(t *testing.T) {
	pkg, err := skillpkg.Build(sources(t), options())
	require.NoError(t, err)

	for _, loc := range pkg.Locales() {
		lp := pkg.Manifest.Manifest.PublishingInformation.Locales[loc]
		assert.Equal(t, "file://assets/images/small-icon.png", lp.SmallIconURI, loc)
		assert.Equal(t, "file://assets/images/large-icon.png", lp.LargeIconURI, loc)
	}

	sizes := map[string]int{"small-icon.png": 108, "large-icon.png": 512}

	for _, f := range pkg.Files {
		name, ok := strings.CutPrefix(f.Name, skillpkg.IconDir)
		if !ok {
			continue
		}

		cfg, format, err := image.DecodeConfig(bytes.NewReader(f.Data))
		require.NoError(t, err, name)
		assert.Equal(t, "png", format, name)
		assert.Equal(t, sizes[name], cfg.Width, name)
		assert.Equal(t, sizes[name], cfg.Height, name)
		delete(sizes, name)
	}

	assert.Empty(t, sizes, "both icons are in the package")
}
