package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFrench(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Québec French says "trois" [tʁwe].
		{"les boys troé", "boys 3"},
		{"Les Trois Accords", "3 accords"},
		// Volumes are parts.
		{"Harmonium volume 2", "harmonium 2"},
		{"le volume deux", "2"},
		{"le volume", "volume"},
		// "vol" is a flight or a theft in French, not only volume.
		{"Vol 93", "vol 93"},
		{"le vol 93", "vol 93"},
		{"Vol 714 pour Sydney", "vol 714 pour sydney"},
	} {
		assert.Equal(t, tc.want, Normalize("fr-CA", tc.in), tc.in)
	}
}
