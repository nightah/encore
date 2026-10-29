package phonetic

// Encoder turns a folded word into phonetic keys.
type Encoder interface {
	// Keys appends the keys for word to dst and returns the extended slice.
	Keys(dst []string, word string) []string
}
