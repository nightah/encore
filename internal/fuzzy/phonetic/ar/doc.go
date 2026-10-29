// Package ar registers the Arabic phonetic encoder: Double Metaphone (package en) with a second key that merges the consonants Arabic speakers and Arabic speech recognition interchange, and a third for the glottal stop Cairene Arabic says for ق.
//
// It keys words as the matcher writes Arabic, in Latin letters (internal/core/match romanizes Arabic to a consonant skeleton, the emphatic consonants already written as their plain pairs, ذ and ظ as th), and English titles as they are, so "تايتانك" (taytank) meets "Titanic" and "قيم" (qim) meets "Game". Double Metaphone already merges what an English title loses in Arabic letters, which has no p, v or e and o: b and p, f and v, and the vowels after the first sound.
//
// The second key merges the dialect pronunciations documented in Janet C. E. Watson, The Phonology and Morphology of Arabic (Oxford University Press, 2002), Clive Holes, Modern Arabic: Structures, Functions, and Varieties (Georgetown University Press, 2004), Bruce Ingham, Najdi Arabic: Central Arabian (John Benjamins, 1994) and Kees Versteegh, The Arabic Language (Edinburgh University Press, 2014):
//
//   - ق said as g in Gulf and Najdi speech and ج said as g in Cairo, which speech recognition writes as ج, ق or ك, and an English g written with any of them: j is keyed as k ("يجابل" for "يقابل", "جيم" for Game).
//   - The interdentals ث, ذ and ظ said as s and z in urban Egyptian and Levantine learned words ("كلسوم" for "كلثوم", "كازم" for "كاظم"): th is keyed as s. Their other merger, with t and d, is Double Metaphone's own second key for th.
//   - خ heard as ح ("احتيار" for "اختيار"): kh is keyed as h.
//
// The third key, for a word with q, says the q as a glottal stop, which Arabic writes as a hamza or not at all ("أصر" for "قصر", "ألبي" for "قلبي"). The rules are written from these descriptions, not ported from any implementation; no Arabic phonetic algorithm was read.
//
// Keys returns at most three keys: Double Metaphone's primary key, the merged key, the glottal key, then Double Metaphone's other keys while there is room.
package ar
