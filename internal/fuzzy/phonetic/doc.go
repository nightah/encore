// Package phonetic defines how words turn into sound-alike keys and which encoder serves which language.
//
// An Encoder appends one or more keys for a word to a caller's slice. Language packages register their encoder at init with Register, and callers resolve one with For, which accepts a language or locale code ("de", "de-DE", "en_GB") and falls back to English. Importing a language package is what makes it available, so a binary carries only the languages it imports.
//
// Every encoder follows one contract: the input is already folded (lowercase, diacritics stripped as match.Normalize does); a word made only of ASCII digits is its own key; a word that encodes to nothing is its own key, so a non-empty word never gets an empty key; there are at most three keys, primary first, without duplicates; keys other than those fallbacks are ASCII; and the encoder is stateless and safe for concurrent use.
package phonetic
