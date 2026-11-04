package docsgen

import (
	"encoding/json"

	"github.com/nightah/encore/internal/skillmodel"
)

// Command is one documented command.
type Command struct {
	Path  string
	Use   string
	Short string
	// Long is split into paragraphs; cobra's own line breaks inside a paragraph are joined.
	Long    []string
	Aliases []string
	Flags   []Flag
	// Runnable is false for a command that only groups subcommands.
	Runnable bool
}

// Flag is one documented flag.
type Flag struct {
	Name      string
	Shorthand string
	Type      string
	Default   string
	Usage     string
}

// ConfigKey is one documented configuration key.
type ConfigKey struct {
	// Key is the dotted path, e.g. server.address or kodi[].url.
	Key     string
	Type    string
	Default string
	// DefaultDoc describes a default that depends on the machine, in place of Default.
	DefaultDoc string
	// Env is the environment variable, or empty when the key can only be set in the config file.
	Env string
	// EnvAliases are shorter environment variables for the same key.
	EnvAliases []string
	Doc        string
}

// ConfigSection is a top-level key and the keys below it.
type ConfigSection struct {
	Key  string
	Doc  string
	Keys []ConfigKey
}

// goDocs holds package config's doc comments: types by name, and fields by type and field name.
type goDocs struct {
	types  map[string]string
	fields map[string]map[string]string
}

type walker struct {
	docs    goDocs
	aliases map[string][]string
	errs    []error
}

// VoiceLocale is one locale of a language, with its launch phrases (skillmodel.Launch) spelled out with its default invocation name.
type VoiceLocale struct {
	Locale     string
	SkillName  string
	Invocation string
	Open       string
	Also       []string
	Skill      string
	// PlainOpenFails marks a locale where a plain open phrase hasn't been seen to reach the skill in Amazon's simulator.
	PlainOpenFails bool
}

// VoiceSection is a group of intents, headed by the comment above its first intent in model.yaml.
type VoiceSection struct {
	Title   string
	Intents []VoiceIntent
}

// VoiceIntent is one intent with the language's utterance patterns.
type VoiceIntent struct {
	Name     string
	Slots    []skillmodel.Slot
	Patterns []string
	// Extra are utterance patterns only some locales add, by locale.
	Extra []LocalePatterns
}

// LocalePatterns are one locale's additional patterns for an intent.
type LocalePatterns struct {
	Locale   string
	Patterns []string
}

// VoiceType is a custom slot type with the language's values.
type VoiceType struct {
	Name    string
	Dynamic bool
	Values  []skillmodel.SlotValue
	// Extra are the synonyms only some locales add, by locale.
	Extra []LocaleValues
}

// LocaleValues are one locale's additional synonyms for a slot type: each value it adds to, with only the synonyms it adds.
type LocaleValues struct {
	Locale string
	Values []skillmodel.SlotValue
}

// APISection is one area of the admin API: an OpenAPI tag.
type APISection struct {
	Title       string
	Description string
	Resources   []APIResource
	Operations  []APIOperation
}

// APIResource is a schema the section's operations return, shown as its keys with the documented fields below.
type APIResource struct {
	Name   string
	Lines  []string
	Fields []APIField
}

// APIField is a documented field of a resource, by its dotted path.
type APIField struct {
	Path string
	Doc  string
}

// APIOperation is one row of a section's table.
type APIOperation struct {
	Method string
	Path   string
	Public bool
	Notes  string
	// Request is the body's shape or media type, and Response the success status and what it carries.
	Request  string
	Response string
	// Params are the notes on its query parameters and request body fields.
	Params []APIField
}

type openAPIDoc struct {
	Tags       []openAPITag                    `json:"tags"`
	Paths      map[string]map[string]openAPIOp `json:"paths"`
	Components struct {
		Schemas map[string]*openAPISchema `json:"schemas"`
	} `json:"components"`
}

type openAPITag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DisplayName any    `json:"x-displayName"`
}

type openAPIOp struct {
	Summary     string                     `json:"summary"`
	Description string                     `json:"description"`
	Tags        []string                   `json:"tags"`
	Security    []map[string][]string      `json:"security"`
	Parameters  []openAPIParam             `json:"parameters"`
	RequestBody *openAPIBody               `json:"requestBody"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIParam struct {
	Name   string         `json:"name"`
	In     string         `json:"in"`
	Schema *openAPISchema `json:"schema"`
}

type openAPIBody struct {
	Content map[string]openAPIMedia `json:"content"`
}

type openAPIResponse struct {
	Content map[string]openAPIMedia `json:"content"`
}

type openAPIMedia struct {
	Schema *openAPISchema `json:"schema"`
}

type openAPISchema struct {
	Ref                  string                    `json:"$ref"`
	Type                 any                       `json:"type"`
	Description          string                    `json:"description"`
	Enum                 []any                     `json:"enum"`
	Default              any                       `json:"default"`
	Items                *openAPISchema            `json:"items"`
	OneOf                []*openAPISchema          `json:"oneOf"`
	Properties           map[string]*openAPISchema `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties json.RawMessage           `json:"additionalProperties"`
	Minimum              *float64                  `json:"minimum"`
	Maximum              *float64                  `json:"maximum"`
}

type apiRenderer struct {
	doc openAPIDoc
	// shown names the schemas that have a resource block of their own, so shapes refer to them by name.
	shown map[string]bool
}

type apiOp struct {
	Method string
	Path   string
	op     openAPIOp
}
