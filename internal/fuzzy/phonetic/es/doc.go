// Package es registers the Spanish phonetic encoder, a consonant skeleton in the style of Double Metaphone with Spanish spelling rules.
//
// The rules are written from standard Spanish grapheme-to-phoneme correspondences and the sound mergers most speakers have, as described in the Real Academia Española's Ortografía de la lengua española (2010): betacism (b, v and w are one sound), silent h, yeísmo (ll and consonant y), seseo (c before e/i, z and s), ch, qu, gu before e/i, g before e/i as j, n before a labial as m, and a final s or d that southern speakers drop. They are not ported from any implementation; Abydos's Spanish Metaphone is GPL-3.0 and was not read. Vowels are dropped after the first sound, as in Double Metaphone.
//
// Keys returns the key without a final s or d first and the key with every letter sounded second.
package es
