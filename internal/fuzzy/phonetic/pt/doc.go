// Package pt registers the Brazilian Portuguese phonetic encoder, a consonant skeleton in the style of Double Metaphone with Brazilian Portuguese spelling-to-sound rules.
//
// The rules are written from the descriptions of Brazilian Portuguese pronunciation in Cristófaro Silva, Fonética e fonologia do português (1999), Mattoso Câmara, Estrutura da língua portuguesa (1970), and Barbosa and Albano, "Brazilian Portuguese", Journal of the International Phonetic Association 34(2) (2004), and from the spelling of the Acordo Ortográfico (1990). They are not ported from any implementation; no Portuguese phonetic algorithm's code (BuscaBR, Brazilian Metaphone ports) was read. Vowels are dropped after the first sound, as in Double Metaphone, so unstressed e and o raised to i and u, dropped accents and tildes, and ei and ou reduced to e and o leave the key as it is.
//
// The spoken (primary) key is the word as a Brazilian says it:
//
//	S   c before e, i or y; s, ss and z, which speech recognition writes for one another ("ç" is already "ss": match.Normalize respells it)
//	X   ch, sh and x ("Xuxa", "chuchu", "Seixas"); x is S after e before a consonant and in a first "ex" ("texto", "exame"), and KS at the end ("tórax")
//	L N lh and nh, so "filhos" meets "filios" and "minha" meets "minia"
//	N   m or n before a consonant or at the end, and a final -ão or -ãos: the nasal ("Jobim", "Jobin", "chefão", "falam")
//	J X d and t before i, a final e or es, a consonant other than r, l or h, or at the end: the affricates of "dia" and "tia" ("Tim" meets "Chim", "cidade" "cidadi", "Djavan" "Diavan"); an epenthetic i after them before a consonant is skipped ("dijavan")
//	J   g before e, i or y, and j; gu before e or i is G, and qu is K
//	J D th, as Brazilians say the "the" of English titles: like the d of "de"
//	-   h; an l before a consonant or at the end, said as u ("Brasil", "Gil", "alto"); an e or i that starts a word before s and a consonant, the epenthetic vowel of "iskank" for Skank; a final r, which Brazilians drop from infinitives ("voltá")
//
// b, v, f, k, p and r keep codes of their own: Brazilian Portuguese has no betacism, and r and rr are written for one another. w and y are vowels.
//
// The spelled (full) key keeps what the spoken one changes from the letters: d and t are D and T, a syllable-final l is L, a final r stays, th is T ("thrones", "trones"), and an h that starts a word before a vowel is R, the Brazilian r an English title's h is said as ("House", "rouse").
//
// Keys returns the spoken key first and the spelled key second when they differ.
package pt
