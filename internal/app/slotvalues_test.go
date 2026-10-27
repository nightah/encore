package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/skillmodel"
)

func TestSlotBuilder(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	b := newSlotBuilder()
	b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", DateAdded: old}, []string{"the spice one"})
	// The same title from a second library merges, keeping the more recent date and both aliases.
	b.add(&media.Item{Kind: media.KindMovie, ID: 9, Title: "dune", DateAdded: old, LastPlayed: recent}, []string{"Dune", "arrakis"})
	// A second library reusing ID 1 for another title gets no ID rather than a wrong one.
	b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Hot Fuzz", DateAdded: old}, nil)
	b.add(&media.Item{Kind: media.KindMovie, ID: 2, Title: "  "}, nil)

	assert.Equal(t, []skillmodel.SlotValue{
		{ID: "movie/1", Value: "Dune", Synonyms: []string{"the spice one", "arrakis"}},
		{Value: "Hot Fuzz"},
	}, b.values(0))

	assert.Equal(t, []skillmodel.SlotValue{{ID: "movie/1", Value: "Dune", Synonyms: []string{"the spice one", "arrakis"}}}, b.values(1), "the cap keeps the most recently played")
}

func TestSlotValuesIgnorePlayback(t *testing.T) {
	build := func(dunePlayed time.Time) []skillmodel.SlotValue {
		b := newSlotBuilder()
		b.add(&media.Item{Kind: media.KindMovie, ID: 1, Title: "Dune", DateAdded: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LastPlayed: dunePlayed}, nil)
		b.add(&media.Item{Kind: media.KindMovie, ID: 2, Title: "Arrival", DateAdded: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}, nil)

		return b.values(0)
	}

	unplayed := build(time.Time{})
	assert.Equal(t, []string{"Arrival", "Dune"}, []string{unplayed[0].Value, unplayed[1].Value}, "sorted by title")
	assert.Equal(t, unplayed, build(time.Date(2026, 10, 27, 10, 53, 0, 0, time.UTC)), "playing a title doesn't change the model")
}
