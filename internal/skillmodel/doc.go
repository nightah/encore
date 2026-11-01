// Package skillmodel builds Alexa interaction models from Encore's language-neutral sources and lints those sources.
//
// The sources live in the i18n directory:
//
//	model.yaml                       intents, their slots and custom slot types
//	lang/<language>/utterances.yaml  utterance patterns per intent (see Expand), keyed by the way of asking
//	lang/<language>/responses.yaml   response strings keyed by message ID
//	lang/<language>/types.yaml       values of static custom slot types, which are words and so per language, keyed by value ID
//	lang/<language>/examples.yaml    example values of dynamic slot types (rooms, playlists, genres), chosen per language rather than translated
//	locales/<locale>.yaml            locale overlay: language, invocation name, extra utterances and synonyms, response overrides
//
// A locale inherits everything from its language and may add utterances, override responses or add synonyms to static slot type values, so en-GB can say "film" where en-US says "movie".
//
// Utterances and slot values are keyed so Crowdin can translate them entry by entry, and each entry may hold several lines, so a language can say one thing in as many ways as it needs:
//
//	WatchMovie:
//	  play: |-
//	    (play|watch) [the] movie {Movie}
//	    put on [the] movie {Movie}
//
// In types.yaml an entry's key is the value's ID, its first line the value and the rest its synonyms. utterances.yaml and types.yaml are translated on Crowdin; examples.yaml isn't, since each language picks examples that suit it.
package skillmodel
