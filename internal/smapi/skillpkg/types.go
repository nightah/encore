package skillpkg

import (
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

// Options are the user's choices for one package.
type Options struct {
	// Locales to include; each must exist in the sources.
	Locales []string
	// Invocation overrides every locale's default invocation name.
	Invocation string
	// SkillName overrides every locale's display name.
	SkillName string
	// SlotValues are supplied at build time, e.g. library titles for AMAZON.Movie. They are cleaned of what Amazon rejects or nobody can say (Kodi formatting tags, control characters, emoji, '*'), and values left empty or too long for Amazon are dropped with a warning. Locales whose recognisers write Latin titles in their own script (ja-JP, hi-IN, ar-SA) also get each value's native-script forms as synonyms, as many as fit without taking the model past WarnModelBytes.
	SlotValues map[string][]skillmodel.SlotValue
	// LocaleSlotValues replaces SlotValues for the locales it names, e.g. to build one locale with fewer titles than the others. Values too long for Amazon are dropped from them too; the warning comes from SlotValues.
	LocaleSlotValues map[string]map[string][]skillmodel.SlotValue
	// Content lists the content groups whose intents the model includes; nil includes every group.
	Content  []media.Content
	Endpoint Endpoint
	Privacy  Privacy
	// Category defaults to DefaultCategory.
	Category string
}

// Endpoint is where Alexa sends the skill's requests.
type Endpoint struct {
	// URI is the public HTTPS URL of Encore's Alexa route, e.g. https://encore.example.com/api/v1/alexa.
	URI string
}

// Privacy holds the user's own answers to Amazon's privacy and compliance questions. Encore never answers them on the user's behalf, so every field except UsesHealthInfo is required.
type Privacy struct {
	AllowsPurchases   *bool `json:"allowsPurchases"`
	UsesPersonalInfo  *bool `json:"usesPersonalInfo"`
	IsChildDirected   *bool `json:"isChildDirected"`
	IsExportCompliant *bool `json:"isExportCompliant"`
	ContainsAds       *bool `json:"containsAds"`
	// UsesHealthInfo is optional; Amazon treats it as false when absent.
	UsesHealthInfo *bool `json:"usesHealthInfo,omitempty"`
}

// Package is a built skill package.
type Package struct {
	Manifest Manifest
	Models   map[string]*skillmodel.InteractionModel
	// Files are the package entries in name order.
	Files []File
	// Warnings are problems that do not stop a deploy, e.g. a model nearing the size limit or dropped slot values.
	Warnings []string

	hash string
}

// File is one entry of the package.
type File struct {
	Name string
	Data []byte
}

// Manifest is skill.json.
type Manifest struct {
	Manifest ManifestBody `json:"manifest"`
}

// ManifestBody is the skill manifest proper.
type ManifestBody struct {
	ManifestVersion       string                `json:"manifestVersion"`
	PublishingInformation PublishingInformation `json:"publishingInformation"`
	PrivacyAndCompliance  Privacy               `json:"privacyAndCompliance"`
	APIs                  APIs                  `json:"apis"`
}

// PublishingInformation describes the skill to the Alexa app. Every interaction model locale must have an entry with a name.
type PublishingInformation struct {
	Locales               map[string]LocalePublishing `json:"locales"`
	IsAvailableWorldwide  bool                        `json:"isAvailableWorldwide"`
	DistributionCountries []string                    `json:"distributionCountries,omitempty"`
	Category              string                      `json:"category"`
}

// LocalePublishing is the per-locale part of PublishingInformation.
type LocalePublishing struct {
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Description    string   `json:"description"`
	ExamplePhrases []string `json:"examplePhrases,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	SmallIconURI   string   `json:"smallIconUri,omitempty"`
	LargeIconURI   string   `json:"largeIconUri,omitempty"`
}

// APIs lists the skill's interfaces; Encore is a custom skill.
type APIs struct {
	Custom CustomAPI `json:"custom"`
}

// CustomAPI is the custom skill interface.
type CustomAPI struct {
	Endpoint   ManifestEndpoint `json:"endpoint"`
	Interfaces []any            `json:"interfaces"`
}

// ManifestEndpoint is the manifest's endpoint object.
type ManifestEndpoint struct {
	URI                string `json:"uri"`
	SSLCertificateType string `json:"sslCertificateType"`
}

// nativeCandidate is one native-script form of a slot value's names that nativeSynonyms may add as a synonym.
type nativeCandidate struct {
	typ   string
	index int
	form  string
	// rank is the form's place among the value's forms, 0 for its own name's likeliest; cost is synonymCost's.
	rank, cost int
}
