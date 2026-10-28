// Package fr registers the French phonetic encoder, a consonant skeleton in the style of Double Metaphone with French spelling rules.
//
// The rules are written from standard French grapheme-to-phoneme correspondences (silent final consonants and plural marks, silent h, soft c and g before e/i/y, ch, ph, gn, qu, -tion, nasal m before b/p), not ported from any implementation. Vowels are dropped after the first sound, as in Double Metaphone, which already merges the spellings of French vowels (é/er/ez/ai, o/au/eau, an/en). Voicing is merged as in Double Metaphone (b/p, d/t, v/f, s/z, hard g/k).
//
// Keys returns the key without the silent ending first and the key with every letter sounded second, so proper names that keep their final consonant still meet.
package fr
