// Package fuzzy is Encore's multi-language fuzzy matching library, used by the title matcher (internal/core/match). Its subpackages are small and independent: phonetic defines the Encoder interface and the language registry, and phonetic/en, phonetic/de, phonetic/fr and phonetic/es each provide one encoder and register it when imported.
package fuzzy
