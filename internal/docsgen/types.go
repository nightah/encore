package docsgen

import (
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

// VoiceLocale is one locale of a language.
type VoiceLocale struct {
	Locale     string
	SkillName  string
	Invocation string
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
}
