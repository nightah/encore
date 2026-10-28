package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func TestEvidenceSummary(t *testing.T) {
	fp := &fingerprintEvidence{Matched: 190, Compared: 200}

	cases := []struct {
		name    string
		ev      evidence
		members int
		want    string
	}{
		{"config none", evidence{Origin: originConfig, None: true}, 1, "kept separate by the config file"},
		{"config group", evidence{Origin: originConfig}, 2, "grouped by the config file"},
		{"config group of one", evidence{Origin: originConfig}, 1, "grouped by the config file"},
		{"user split", evidence{Origin: originUser}, 1, "kept separate by the user"},
		{"user group", evidence{Origin: originUser, Fingerprint: fp}, 3, "grouped by the user"},
		{"detected solo", evidence{Origin: originDetected, Fingerprint: fp, Confirmation: "seen"}, 1, ""},
		{"confirmed beats fingerprint", evidence{Origin: originDetected, Fingerprint: fp, Confirmation: "a change was seen"}, 2, "a change was seen"},
		{"fingerprint", evidence{Origin: originDetected, Fingerprint: fp}, 2, "190 of 200 sampled items match"},
		{"nothing", evidence{}, 2, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.ev.summary(c.members))
		})
	}
}

func TestEvidenceRoundTrip(t *testing.T) {
	ev := evidence{Origin: originDetected, Confirmed: []int64{1, 2}, Vetoed: []int64{3}, Reason: "a change to “Dune” wasn't seen"}

	assert.Equal(t, ev, decodeEvidence(ev.encode()))
	assert.Equal(t, evidence{}, decodeEvidence(nil))
	assert.Equal(t, evidence{}, decodeEvidence([]byte(`{"origin":`)), "damaged evidence starts over")

	c := ev.clone()
	c.Confirmed[0], c.Vetoed[0] = 9, 9
	assert.Equal(t, []int64{1, 2}, ev.Confirmed, "a clone doesn't share its lists")
	assert.Equal(t, []int64{3}, ev.Vetoed)
}

func TestGroupsDescribeEachMember(t *testing.T) {
	lib := testLibrary(30, 0, "2024-01-01 10:00:00")
	study := fakekodi.New(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})
	lounge, bedroom := fakekodi.New(t, fakekodi.Options{Library: lib}), fakekodi.New(t, fakekodi.Options{Library: lib})
	a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Study", study, ""), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

	require.Eventually(t, func() bool {
		g, _ := a.groupOf("Lounge")
		return len(g.Members) == 2 && a.fingerprintedAll("Study", "Lounge", "Bedroom")
	}, eventually, 10*time.Millisecond)

	inst := a.instanceNamed(t, "Lounge")

	a.mu.RLock()
	g := inst.group
	rec, ev := g.rec, g.ev.clone()
	a.mu.RUnlock()

	ev.Confirmed = []int64{inst.id}

	a.planMu.Lock()
	err := a.saveGroup(t.Context(), g, rec, ev)
	a.planMu.Unlock()
	require.NoError(t, err)

	groups := a.Groups()
	require.Len(t, groups, 2)
	assert.Equal(t, []string{"Lounge", "Study"}, []string{groups[0].Name, groups[1].Name}, "ordered by name, not by when they were made")

	confirmed := map[string]bool{}

	for _, m := range groups[0].Members {
		confirmed[m.Name] = m.Confirmed
		assert.True(t, m.Fingerprinted, m.Name)
	}

	assert.Equal(t, map[string]bool{"Lounge": true, "Bedroom": false}, confirmed, "only members seen mirroring a change are confirmed")
	assert.Equal(t, inst.id, groups[0].SourceID)
	assert.Equal(t, inst.id, groups[0].ActiveID)
}
