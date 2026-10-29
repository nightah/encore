package skeleton

import "bytes"

// Emit appends a consonant code unless it repeats the previous one with no vowel between, so doubled letters and digraphs that land on one sound count once.
func (k *Key) Emit(codes ...byte) {
	for _, c := range codes {
		if c == k.last || k.n == len(k.buf) {
			continue
		}

		k.buf[k.n] = c
		k.n++
		k.last = c
	}
}

// Vowel records a vowel: the first sound of the key becomes "A", as in Double Metaphone, and later vowels only separate consonants.
func (k *Key) Vowel() {
	if k.n == 0 {
		k.Emit('A')
	}

	k.last = 0
}

// Bytes is the key so far.
func (k *Key) Bytes() []byte { return k.buf[:k.n] }

// Append adds the primary key (the word without its silent ending) and, when it differs, the full key (every letter sounded) to dst. A word whose keys are both empty is its own key; all-digit words are the caller's to pass through.
func Append(dst []string, word string, primary, full *Key) []string {
	p, f := primary.Bytes(), full.Bytes()

	switch {
	case len(p) == 0 && len(f) == 0:
		return append(dst, word)
	case len(p) == 0:
		return append(dst, string(f))
	}

	dst = append(dst, string(p))
	if len(f) > 0 && !bytes.Equal(f, p) {
		dst = append(dst, string(f))
	}

	return dst
}

// At is w[i], or 0 outside w.
func At(w string, i int) byte {
	if i < 0 || i >= len(w) {
		return 0
	}

	return w[i]
}
