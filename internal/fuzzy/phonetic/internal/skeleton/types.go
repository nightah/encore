package skeleton

// Key is a phonetic key built on the stack, at most MaxLen codes long; codes past that are dropped.
type Key struct {
	buf  [MaxLen]byte
	n    int
	last byte
}
