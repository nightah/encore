package skeleton

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func key(codes string) *Key {
	var k Key

	for i := range len(codes) {
		if codes[i] == '_' {
			k.Vowel()
		} else {
			k.Emit(codes[i])
		}
	}

	return &k
}

func TestKey(t *testing.T) {
	assert.Equal(t, "A", string(key("_").Bytes()), "a leading vowel is A")
	assert.Equal(t, "APT", string(key("_P_T").Bytes()), "later vowels only separate")
	assert.Equal(t, "PT", string(key("PPT").Bytes()), "repeats collapse")
	assert.Equal(t, "PP", string(key("P_P").Bytes()), "a vowel keeps repeats apart")
	assert.Len(t, key(strings.Repeat("PT", MaxLen)).Bytes(), MaxLen, "keys stop at MaxLen")

	var k Key

	k.Emit('K', 'S')
	assert.Equal(t, "KS", string(k.Bytes()))
}

func TestAppend(t *testing.T) {
	assert.Equal(t, []string{"w"}, Append(nil, "w", key(""), key("")), "no key falls back to the word")
	assert.Equal(t, []string{"PT"}, Append(nil, "w", key(""), key("PT")))
	assert.Equal(t, []string{"P"}, Append(nil, "w", key("P"), key("")))
	assert.Equal(t, []string{"P"}, Append(nil, "w", key("P"), key("P")), "an equal full key is not repeated")
	assert.Equal(t, []string{"x", "P", "PT"}, Append([]string{"x"}, "w", key("P"), key("PT")))
}

func TestAt(t *testing.T) {
	assert.Equal(t, byte('b'), At("ab", 1))
	assert.Equal(t, byte(0), At("ab", 2))
	assert.Equal(t, byte(0), At("ab", -1))
}
