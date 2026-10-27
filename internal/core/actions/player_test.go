package actions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/testkit"
)

// playingTarget is a target with Hot Fuzz playing.
func playingTarget(t *testing.T) (Target, *testkit.Backend) {
	t.Helper()

	target, backend, _ := newTarget()
	require.NoError(t, backend.Play(t.Context(), media.Ref{Kind: media.KindMovie, ID: 4}, media.PlayOptions{}))

	return target, backend
}

func TestSeek(t *testing.T) {
	idle, _, _ := newTarget()
	svc := newService()

	o := svc.Seek(t.Context(), idle, SeekRequest{By: time.Minute})
	assert.Equal(t, "playback.nothing_playing", o.Say.ID)

	target, backend := playingTarget(t)
	to := 42 * time.Minute

	for _, req := range []SeekRequest{
		{By: 90 * time.Second},
		{By: -10 * time.Minute},
		{Step: media.SeekBigBackward},
		{},
		{To: &to},
	} {
		o = svc.Seek(t.Context(), target, req)
		assert.Empty(t, o.Say.ID, "seeking is silent")
		assert.True(t, o.EndSession)
	}

	assert.Equal(t, []string{"Play movie/4", "Seek by 1m30s", "Seek by -10m0s", "Seek bigbackward", "Seek smallforward", "Seek to 42m0s"}, backend.Calls())
}

func TestShuffleAndRepeat(t *testing.T) {
	target, _ := playingTarget(t)
	svc := newService()

	assert.Equal(t, "player.shuffle_on", svc.Shuffle(t.Context(), target, true).Say.ID)
	assert.Equal(t, "player.shuffle_off", svc.Shuffle(t.Context(), target, false).Say.ID)

	assert.Equal(t, "player.repeat_one", svc.Repeat(t.Context(), target, media.RepeatCycle).Say.ID, "loop on cycles off → one")
	assert.Equal(t, "player.repeat_all", svc.Repeat(t.Context(), target, media.RepeatCycle).Say.ID)
	assert.Equal(t, "player.repeat_off", svc.Repeat(t.Context(), target, media.RepeatCycle).Say.ID)
	assert.Equal(t, "player.repeat_all", svc.Repeat(t.Context(), target, media.RepeatAll).Say.ID)
	assert.Equal(t, "player.repeat_off", svc.Repeat(t.Context(), target, media.RepeatOff).Say.ID)

	idle, _, _ := newTarget()
	assert.Equal(t, "playback.nothing_playing", svc.Shuffle(t.Context(), idle, true).Say.ID)
	assert.Equal(t, "playback.nothing_playing", svc.Repeat(t.Context(), idle, media.RepeatCycle).Say.ID)
}

func TestPictureKeepsSessionOpen(t *testing.T) {
	target, backend := playingTarget(t)

	o := newService().Picture(t.Context(), target, media.PictureZoomIn, media.PictureMoveUp)
	assert.Empty(t, o.Say.ID)
	assert.False(t, o.EndSession, "the next adjustment needs no wake word")
	assert.Equal(t, []string{"Play movie/4", "Picture zoomin", "Picture moveup"}, backend.Calls())
}

func TestToggle(t *testing.T) {
	target, backend, _ := newTarget()

	o := newService().Toggle(t.Context(), target, media.InputPassthrough)
	assert.Empty(t, o.Say.ID)
	assert.Equal(t, []string{"Input passthrough"}, backend.Calls())
}

func TestTimeRemaining(t *testing.T) {
	target, backend, _ := newTarget()
	svc := newService()

	assert.Equal(t, "playback.nothing_playing", svc.TimeRemaining(t.Context(), target).Say.ID)

	backend.SetPlaying(&media.NowPlaying{Item: testkit.SampleLibrary()[3], Position: 79 * time.Minute, Duration: 121 * time.Minute})
	backend.SetClock(time.Date(1, 1, 1, 21, 17, 40, 0, time.UTC))

	o := svc.TimeRemaining(t.Context(), target)
	assert.Equal(t, "player.remaining_until", o.Say.ID)
	assert.Equal(t, 42*time.Minute, o.Say.Durations["remaining"])
	assert.Equal(t, "22:00", o.Say.Times["end"].Format("15:04"), "rounded to the minute")

	backend.SetClock(time.Time{})

	o = svc.TimeRemaining(t.Context(), target)
	assert.Equal(t, "player.remaining", o.Say.ID, "without a clock it just says how long")

	backend.SetPlaying(&media.NowPlaying{Item: testkit.SampleLibrary()[3], Position: 120*time.Minute + 30*time.Second, Duration: 121 * time.Minute})
	assert.Equal(t, "player.nearly_over", svc.TimeRemaining(t.Context(), target).Say.ID)
}
