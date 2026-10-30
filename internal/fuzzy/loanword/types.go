package loanword

// phone is one sound of an English word as english reads it: a consonant ("k", "sh", "ng"), a vowel ("a", "ii", "ai"), schwa ("@"), "R", an r after a vowel, which non-rhotic speech and Japanese write as a long vowel, or "Q", which marks a doubled consonant.
type phone = string

// word is one Latin-script word of a title and how Native writes it.
type word struct {
	// text is the word as the title spells it.
	text string
	// letters is set for an initialism, spelt out by letter name ("BTS", "E.T.").
	letters bool
	// keep is set for a word written as it is: a roman numeral.
	keep bool
}

// writer writes one language's native script from a word.
type writer struct {
	// english writes a word read as English.
	english func(phones []phone) string
	// letters spells an initialism; joined writes the letter names without spaces between them.
	letters map[byte]string
	joined  bool
	// romanised writes a word as a romanisation of the language, letter by letter; nil when the language has none Native reads.
	romanised func(w string) string
	// dropThe also writes a title without a leading "The", which Japanese release names often leave out.
	dropThe bool
}

// piece is a Latin word of a title (w set) or the text between such words, kept as it is.
type piece struct {
	w    *word
	text string
}

// reader reads one word by English spelling rules, left to right.
type reader struct {
	w   string
	i   int
	out []phone
	// long is the index of the vowel a silent final e makes long, or -1.
	long int
	// silent is set when the word ends in a silent e: after a consonant, in a word with another vowel.
	silent bool
}

// devVowel is a vowel's letter and sign.
type devVowel struct{ letter, matra string }
