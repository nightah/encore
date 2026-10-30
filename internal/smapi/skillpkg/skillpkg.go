package skillpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/skillmodel"
)

// Build validates opts and assembles the package for the chosen locales.
func Build(src *skillmodel.Sources, opts Options) (*Package, error) {
	if len(opts.Locales) == 0 {
		return nil, errors.New("skillpkg: no locales selected")
	}

	if err := opts.Privacy.validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPrivacyUnanswered, err)
	}

	if err := opts.Endpoint.validate(); err != nil {
		return nil, fmt.Errorf("skillpkg: %w", err)
	}

	locales := slices.Clone(opts.Locales)
	slices.Sort(locales)
	locales = slices.Compact(locales)

	slotValues, warnings := filterSlotValues(opts.SlotValues)

	category := opts.Category
	if category == "" {
		category = DefaultCategory
	}

	pkg := &Package{
		Models:   make(map[string]*skillmodel.InteractionModel, len(locales)),
		Warnings: warnings,
	}

	body := &pkg.Manifest.Manifest
	body.ManifestVersion = "1.0"
	body.PublishingInformation = PublishingInformation{Locales: map[string]LocalePublishing{}, Category: category}
	body.PrivacyAndCompliance = opts.Privacy
	body.APIs.Custom = CustomAPI{
		Endpoint:   ManifestEndpoint{URI: opts.Endpoint.URI, SSLCertificateType: sslCertificateType},
		Interfaces: []any{},
	}

	countries := map[string]bool{}

	for _, locale := range locales {
		loc, ok := src.Locales[locale]
		if !ok {
			return nil, fmt.Errorf("skillpkg: unknown locale %q", locale)
		}

		values := slotValues
		if own, ok := opts.LocaleSlotValues[locale]; ok {
			values, _ = filterSlotValues(own)
		}

		model, err := skillmodel.Build(src, locale, skillmodel.BuildOptions{Invocation: opts.Invocation, SlotValues: values, Content: opts.Content})
		if err != nil {
			return nil, fmt.Errorf("skillpkg: %w", err)
		}

		data, warns, err := encodeModel(locale, model)
		if err != nil {
			return nil, err
		}

		pkg.Models[locale] = model
		pkg.Warnings = append(pkg.Warnings, warns...)
		pkg.Files = append(pkg.Files, File{Name: ModelDir + locale + ".json", Data: data})

		name := loc.SkillName
		if opts.SkillName != "" {
			name = opts.SkillName
		}

		if len(name) < 2 {
			return nil, fmt.Errorf("skillpkg: locale %s: skill name %q is too short", locale, name)
		}

		body.PublishingInformation.Locales[locale] = localePublishing(name, loc.Language, model.InteractionModel.LanguageModel.InvocationName)

		if c := country(locale); c != "" {
			countries[c] = true
		}
	}

	body.PublishingInformation.DistributionCountries = slices.Sorted(maps.Keys(countries))

	manifest, err := json.MarshalIndent(pkg.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("skillpkg: encoding manifest: %w", err)
	}

	pkg.Files = append(pkg.Files, File{Name: ManifestPath, Data: manifest})
	pkg.Files = append(pkg.Files, iconFiles()...)
	slices.SortFunc(pkg.Files, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
	pkg.hash = hashFiles(pkg.Files)

	return pkg, nil
}

// Hash identifies the package's content. It is stable across builds and Go versions, unlike the zip's bytes might be.
func (p *Package) Hash() string {
	return p.hash
}

// Locales returns the package's locales in order.
func (p *Package) Locales() []string {
	return slices.Sorted(maps.Keys(p.Models))
}

// Invocation returns the invocation name for locale, or "".
func (p *Package) Invocation(locale string) string {
	m, ok := p.Models[locale]
	if !ok {
		return ""
	}

	return m.InteractionModel.LanguageModel.InvocationName
}
