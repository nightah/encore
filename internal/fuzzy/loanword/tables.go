package loanword

import "strings"

// englishWords are common words whose sound their spelling doesn't give by english's rules, and the abbreviations titles use, as phones.
var englishWords = phoneTable(map[string]string{
	"the": "dh @", "a": "@", "an": "ae n", "of": "o v", "to": "t uu", "you": "y uu", "your": "y oo R", "one": "w a n",
	"two": "t uu", "for": "f oo R", "four": "f oo R", "are": "aa R", "do": "d uu", "who": "h uu", "what": "w o t",
	"love": "l a v", "come": "k a m", "some": "s a m", "done": "d a n", "gone": "g o n", "have": "h ae v", "give": "g i v",
	"live": "l i v", "move": "m uu v", "said": "s e d", "says": "s e z", "does": "d a z", "eye": "ai", "people": "p ii p @ l",
	"world": "w er R l d", "word": "w er R d", "work": "w er R k", "war": "w oo R", "water": "w oo t @ R",
	"father": "f aa dh @ R", "mother": "m a dh @ R", "brother": "b r a dh @ R", "other": "a dh @ R", "woman": "w u m @ n",
	"women": "w i m i n", "again": "@ g e n", "any": "e n ii", "many": "m e n ii", "only": "ou n l ii", "once": "w a n s",
	"there": "dh ea R", "where": "w ea R", "here": "h ia R", "were": "w er R", "through": "th r uu", "though": "dh ou",
	"friend": "f r e n d", "island": "ai l @ n d", "great": "g r ei t", "break": "b r ei k", "bear": "b ea R", "wear": "w ea R",
	"sword": "s oo R d", "ocean": "ou sh @ n", "sugar": "sh u g @ R", "sure": "sh oo R", "this": "dh i s", "that": "dh ae t",
	"they": "dh ei", "them": "dh e m", "then": "dh e n", "than": "dh ae n", "these": "dh ii z", "those": "dh ou z",
	"with": "w i dh", "i": "ai", "is": "i z", "his": "h i z", "has": "h ae z", "was": "w o z", "as": "ae z", "iron": "ai @ R n",
	"ok": "ou k ei", "no": "n ou", "mr": "m i s t @ R", "mrs": "m i s i z", "ms": "m i z", "dr": "d o k t @ R",
	"vs": "v er R s @ s", "jr": "j uu n i @ R", "vol": "v o l y uu m", "christmas": "k r i s m @ s", "girl": "g er R l",
	"bird": "b er R d", "heart": "h aa R t", "pretty": "p r i t ii", "busy": "b i z ii", "build": "b i l d",
	"son": "s a n", "won": "w a n", "money": "m a n ii", "nothing": "n a th i ng", "london": "l a n d @ n", "front": "f r a n t",
	"month": "m a n th", "wonder": "w a n d @ R", "young": "y a ng", "touch": "t a ch", "country": "k a n t r ii",
	"double": "d a b @ l", "trouble": "t r a b @ l", "group": "g r uu p", "soul": "s ou l", "could": "k u d", "would": "w u d",
	"should": "sh u d", "put": "p u t", "push": "p u sh", "pull": "p u l", "full": "f u l", "good": "g u d", "foot": "f u t",
	"blood": "b l a d", "door": "d oo R", "floor": "f l oo R", "machine": "m @ sh ii n", "police": "p @ l ii s",
	"magic": "m ae j i k", "music": "m y uu z i k", "radio": "r ei d ii ou", "video": "v i d ii ou", "piano": "p ii ae n ou",
	"lady": "l ei d ii", "baby": "b ei b ii", "crazy": "k r ei z ii", "paper": "p ei p @ R", "danger": "d ei n j @ R",
	"stranger": "s t r ei n j @ R", "angel": "ei n j @ l", "change": "ch ei n j", "strange": "s t r ei n j", "range": "r ei n j",
	"apple": "ae p @ l", "rock": "r o k", "back": "b ae k", "wednesday": "w e n z d ei", "oppenheimer": "o p @ n h ai m @ R",
	"begin": "b i g i n", "begins": "b i g i n z", "forgive": "f @ R g i v", "together": "t @ g e dh @ R",
	"forget": "f @ R g e t", "target": "t aa R g i t", "finger": "f i ng g @ R", "hunger": "h a ng g @ R",
	"hungry": "h a ng g r ii", "angry": "ae ng g r ii", "japan": "j @ p ae n",
})

// ownWords are the words in which "own" is said as in "known", not as in "down".
var ownWords = map[string]bool{
	"own": true, "known": true, "shown": true, "grown": true, "thrown": true, "flown": true, "blown": true, "sown": true,
}

// romanNumerals are the roman numerals Native keeps as they are, as the matcher reads them as numbers.
var romanNumerals = map[string]bool{
	"II": true, "III": true, "IV": true, "VI": true, "VII": true, "VIII": true, "IX": true, "XI": true, "XII": true,
	"XIII": true, "XIV": true, "XV": true, "XVI": true, "XVII": true, "XVIII": true, "XIX": true, "XX": true,
}

// codas are the pairs of consonant letters an English syllable can end in or start with; an all-capital word with a pair outside them can't be said as a word and is spelt out ("AKB").
var codas = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields("bl br ch ck cl cr ct dr fl fr ft gh gl gn gr kn ld lf lk ll lm lp lt mb mp nc nd ng nk ns nt ph pl pr ps pt rb rc rd rf rg rk rl rm rn rp rs rt sc sh sk sl sm sn sp ss st sw th tr ts tw wh wn wr xt") {
		m[p] = true
	}

	return m
}()

func phoneTable(m map[string]string) map[string][]phone {
	out := make(map[string][]phone, len(m))
	for w, p := range m {
		out[w] = strings.Fields(p)
	}

	return out
}
