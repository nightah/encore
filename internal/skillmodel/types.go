package skillmodel

import "github.com/nightah/encore/internal/core/media"

// Sources is everything loaded from the i18n directory.
type Sources struct {
	Model     Model
	Languages map[string]Language
	Locales   map[string]Locale
}

// Model is the language-neutral part of the interaction model.
type Model struct {
	Intents []Intent   `yaml:"intents"`
	Types   []SlotType `yaml:"types"`
}

// Intent is one intent and the slots its samples may reference.
type Intent struct {
	Name  string `yaml:"name"`
	Slots []Slot `yaml:"slots"`
	// Content is the group the intent belongs to (movies, tv or music); the skill can be built without a group's intents. Empty means the intent is always included.
	Content media.Content `yaml:"content"`
}

// Slot is a named, typed placeholder in an intent's samples.
type Slot struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// Fallback is the custom type the slot takes in a locale that lacks Type, a built-in type (see LacksBuiltin). The slot keeps its name, so its handler reads it the same way.
	Fallback string `yaml:"fallback"`
}

// SlotType is a custom slot type. Values may be static here, per language in types.yaml, or supplied at build time, e.g. room names.
type SlotType struct {
	Name   string      `yaml:"name"`
	Values []SlotValue `yaml:"values"`
	// Dynamic types get their values at build time, e.g. the rooms set up in Encore. A language's values for them are examples, used only when the build supplies none, so the model still builds before anything is configured.
	Dynamic bool `yaml:"dynamic"`
	// From names slot types whose build-time values a dynamic type takes when none are supplied for it: merged, one per value whatever its case, and sorted. Limit caps how many, picked evenly across the sorted values so no stretch of the alphabet is left out; zero means all.
	From  []string `yaml:"from"`
	Limit int      `yaml:"limit"`
}

// SlotValue is one value of a slot type, with an optional stable ID that entity resolution hands back to the skill.
type SlotValue struct {
	ID       string   `yaml:"id"`
	Value    string   `yaml:"value"`
	Synonyms []string `yaml:"synonyms"`
}

// Language holds the strings shared by every locale of one language.
type Language struct {
	// Utterances are each intent's patterns, every entry's lines in file order.
	Utterances map[string][]string
	Responses  map[string]string
	// Types are the language's values for custom slot types, by type name.
	Types map[string][]SlotValue
	// Examples are the dynamic types' example values from examples.yaml, also included in Types.
	Examples map[string][]SlotValue
	// UtteranceKeys and TypeKeys are each intent's and static slot type's entry keys in file order. Crowdin writes only the reference language's keys, so the linter rejects any other.
	UtteranceKeys map[string][]string
	TypeKeys      map[string][]string
}

// Locale is a per-locale overlay on its language.
type Locale struct {
	Language   string `yaml:"language"`
	SkillName  string `yaml:"skill_name"`
	Invocation string `yaml:"invocation"`
	// KodiInvocation is how the locale writes the "kodi" invocation choice (InvocationKodi), for a language whose script can't use the Latin name. Empty means "kodi".
	KodiInvocation string              `yaml:"kodi_invocation"`
	Utterances     map[string][]string `yaml:"utterances"`
	Responses      map[string]string   `yaml:"responses"`
}

// keyedSection is one intent or slot type of a keyed file, its entries in file order.
type keyedSection struct {
	name    string
	entries []keyedEntry
}

// keyedEntry is one key and its non-empty lines.
type keyedEntry struct {
	key   string
	lines []string
}

// InteractionModel is the JSON document SMAPI accepts for one locale.
type InteractionModel struct {
	InteractionModel struct {
		LanguageModel LanguageModel `json:"languageModel"`
	} `json:"interactionModel"`
}

// LanguageModel is the interaction model's language model section.
type LanguageModel struct {
	InvocationName string        `json:"invocationName"`
	Intents        []ModelIntent `json:"intents"`
	Types          []ModelType   `json:"types,omitempty"`
}

// ModelIntent is an intent as SMAPI expects it.
type ModelIntent struct {
	Name    string      `json:"name"`
	Slots   []ModelSlot `json:"slots,omitempty"`
	Samples []string    `json:"samples"`
}

// ModelSlot is a slot as SMAPI expects it.
type ModelSlot struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ModelType is a slot type as SMAPI expects it.
type ModelType struct {
	Name   string           `json:"name"`
	Values []ModelTypeValue `json:"values"`
}

// ModelTypeValue is a slot type value as SMAPI expects it.
type ModelTypeValue struct {
	ID   string `json:"id,omitempty"`
	Name struct {
		Value    string   `json:"value"`
		Synonyms []string `json:"synonyms,omitempty"`
	} `json:"name"`
}

// BuildOptions are the user-specific inputs to a build.
type BuildOptions struct {
	// Invocation overrides the locale's default invocation name. InvocationKodi picks the locale's own spelling of it (Locale.KodiInvocation).
	Invocation string
	// SlotValues supplies values per slot type at build time, for custom types and for extending built-in types such as AMAZON.Movie with library titles. For a dynamic type they replace the language's example values.
	SlotValues map[string][]SlotValue
	// Content lists the content groups to include; nil includes every group. Intents of other groups are left out, along with slot types only they used.
	Content []media.Content
}

type parser struct {
	src string
	pos int
}

// Severity grades a lint finding.
type Severity int

// Finding is one lint result. Locale and Intent are empty when not relevant.
type Finding struct {
	Severity Severity
	Locale   string
	Intent   string
	Message  string
}

type linter struct {
	src      *Sources
	findings []Finding
	// script is the rules of the language of the locale being checked.
	script scriptRules
}

// scriptRules are the invocation name and sample rules of the languages written in one script.
type scriptRules struct {
	// invocation reports whether a name uses only what Amazon allows in the script, and message says what that is.
	invocation func(name string) bool
	message    string
	// unspaced scripts write words without spaces (Japanese): particles attach straight to a slot, a reserved word counts anywhere in a name, and a name has no words to count.
	unspaced bool
}

type sampleOwner struct {
	intent  string
	pattern string
}
