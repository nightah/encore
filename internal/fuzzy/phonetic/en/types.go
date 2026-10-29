package en

// Encoder is English Double Metaphone (Philips 2000) with unbounded code length, plus an R-less key for non-rhotic speakers. The zero value is ready to use.
type Encoder struct{}

// metaphone is one Double Metaphone run over the upper-cased letters of a word.
type metaphone struct {
	s        []rune
	n, last  int
	i        int
	pri, alt code
	slavo    bool
}

// code is a growing Double Metaphone code that stays on the stack until it outgrows buf. Appending to a slice field through a pointer would move every buffer to the heap.
type code struct {
	buf  [stackCode]byte
	n    int
	heap []byte
}
