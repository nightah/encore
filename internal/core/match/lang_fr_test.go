package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFrench(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		// Québec French says "trois" [tʁwe].
		{name: "ShouldNormalizeLesBoysTroe", have: "les boys troé", expected: "boys 3"},
		{name: "ShouldNormalizeLesTroisAccords", have: "Les Trois Accords", expected: "3 accords"},
		// Volumes are parts.
		{name: "ShouldNormalizeHarmoniumVolume2", have: "Harmonium volume 2", expected: "harmonium 2"},
		{name: "ShouldReadVolumeAsPart", have: "le volume deux", expected: "2"},
		{name: "ShouldKeepVolumeAlone", have: "le volume", expected: "volume"},
		// "vol" is a flight or a theft in French, not only volume.
		{name: "ShouldNormalizeVol93", have: "Vol 93", expected: "vol 93"},
		{name: "ShouldNormalizeLeVol93", have: "le vol 93", expected: "vol 93"},
		{name: "ShouldNormalizeVol714PourSydney", have: "Vol 714 pour Sydney", expected: "vol 714 pour sydney"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Normalize("fr-CA", tc.have))
		})
	}
}
