package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
