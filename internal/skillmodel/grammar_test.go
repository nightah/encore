package skillmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		pattern string
		want    []string
	}{
		{"pause", []string{"pause"}},
		{"(play|watch) {Movie}", []string{"play {Movie}", "watch {Movie}"}},
		{"play [the] movie {Movie}", []string{"play the movie {Movie}", "play movie {Movie}"}},
		{"[please] (play|put on) [the (movie|film)] {Movie}", []string{
			"please play the movie {Movie}", "please play the film {Movie}", "please play {Movie}",
			"please put on the movie {Movie}", "please put on the film {Movie}", "please put on {Movie}",
			"play the movie {Movie}", "play the film {Movie}", "play {Movie}",
			"put on the movie {Movie}", "put on the film {Movie}", "put on {Movie}",
		}},
		{"(turn|switch) [the] volume (up|down)", []string{
			"turn the volume up", "turn the volume down", "turn volume up", "turn volume down",
			"switch the volume up", "switch the volume down", "switch volume up", "switch volume down",
		}},
		{"what's   playing", []string{"what's playing"}},
		{"(a|a) b", []string{"a b"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got, err := Expand(tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandErrors(t *testing.T) {
	for _, pattern := range []string{
		"(play|watch {Movie}",
		"play [the movie {Movie}",
		"play {Movie",
		"play | watch",
		"play)",
		"[]",
		strings.Repeat("(a|b|c|d|e) ", 6),
	} {
		t.Run(pattern, func(t *testing.T) {
			_, err := Expand(pattern)
			assert.Error(t, err)
		})
	}
}
