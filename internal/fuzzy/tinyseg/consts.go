package tinyseg

// bias is the original's BIAS, the score of a boundary before any weight is added.
const bias = -332

// pad is how many tags stand before and after the text.
const pad = 3

// The padding tags the original writes B3 B2 B1 before the text and E1 E2 E3 after it, as private-use runes so they never meet a real character.
const (
	tagB1 rune = 0xE001 + iota
	tagB2
	tagB3
	tagE1
	tagE2
	tagE3
)

// runeBits is how many bits a packed character takes.
const runeBits = 21

// stackChars is the longest text, padding included, that Append segments without allocating its working arrays.
const stackChars = 64

// numerals are the kanji the original classes as numerals.
const numerals = "一二三四五六七八九十百千万億兆"
