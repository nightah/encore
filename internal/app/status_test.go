package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
)

func TestEvidenceSummary(t *testing.T) {
	fp := &fingerprintEvidence{Matched: 190, Compared: 200}

	testCases := []struct {
		name     string
		have     evidence
		members  int
		expected string
	}{
		{name: "ShouldSummariseAConfigSeparation", have: evidence{Origin: originConfig, None: true}, members: 1, expected: "kept separate by the config file"},
		{name: "ShouldSummariseAConfigGroup", have: evidence{Origin: originConfig}, members: 2, expected: "grouped by the config file"},
		{name: "ShouldSummariseAConfigGroupOfOne", have: evidence{Origin: originConfig}, members: 1, expected: "grouped by the config file"},
		{name: "ShouldSummariseAUserSplit", have: evidence{Origin: originUser}, members: 1, expected: "kept separate by the user"},
		{name: "ShouldSummariseAUserGroup", have: evidence{Origin: originUser, Fingerprint: fp}, members: 3, expected: "grouped by the user"},
		{name: "ShouldSummariseNothingForADetectedSolo", have: evidence{Origin: originDetected, Fingerprint: fp, Confirmation: "seen"}, members: 1},
		{name: "ShouldPreferTheConfirmationOverTheFingerprint", have: evidence{Origin: originDetected, Fingerprint: fp, Confirmation: "a change was seen"}, members: 2, expected: "a change was seen"},
		{name: "ShouldSummariseTheFingerprint", have: evidence{Origin: originDetected, Fingerprint: fp}, members: 2, expected: "190 of 200 sampled items match"},
		{name: "ShouldSummariseNothingWithoutEvidence", have: evidence{}, members: 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.have.summary(tc.members))
		})
	}
}

func TestLibrariesAreOrderedByGroup(t *testing.T) {
	names := []string{"Study", "Lounge", "Bedroom", "Den"}

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	for _, name := range names {
		cfg.Kodi = append(cfg.Kodi, kodiConfig(name, fakekodi.New(t, fakekodi.Options{}), ""))
	}

	// Not run, so no instances are grouped yet: each has a library of its own.
	a := openApp(t, &cfg, fastTuning())
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	// The groups are held in a map, whose order changes from one range to the next; the list's must not.
	for range 20 {
		libs := a.Libraries()
		require.Len(t, libs, len(names))

		got := make([]string, 0, len(libs))

		for i, lib := range libs {
			if i > 0 {
				require.Less(t, libs[i-1].GroupID, lib.GroupID, "ordered by group ID")
			}

			require.Len(t, lib.Members, 1)
			got = append(got, lib.Members[0])
		}

		require.ElementsMatch(t, names, got, "one library per instance")
	}
}

func TestLibrariesNameTheirSourceAndLanguage(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()

	ja := kodiConfig("Lounge", fakekodi.New(t, fakekodi.Options{}), "")
	ja.Language = "ja"
	cfg.Kodi = append(cfg.Kodi, ja, kodiConfig("Study", fakekodi.New(t, fakekodi.Options{}), ""))

	a := openApp(t, &cfg, fastTuning())
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	got := map[string]string{}
	for _, lib := range a.Libraries() {
		got[lib.Source] = lib.Language
	}

	assert.Equal(t, map[string]string{"Lounge": "ja", "Study": "en"}, got)
}

func TestEvidenceRoundTrip(t *testing.T) {
	ev := evidence{Origin: originDetected, Confirmed: []int64{1, 2}, Vetoed: []int64{3}, Reason: "a change to “Dune” wasn't seen"}

	testCases := []struct {
		name     string
		have     []byte
		expected evidence
	}{
		{name: "ShouldRoundTrip", have: ev.encode(), expected: ev},
		{name: "ShouldDecodeNothingAsEmpty"},
		{name: "ShouldStartDamagedEvidenceOver", have: []byte(`{"origin":`)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, decodeEvidence(tc.have))
		})
	}

	c := ev.clone()
	c.Confirmed[0], c.Vetoed[0] = 9, 9
	assert.Equal(t, []int64{1, 2}, ev.Confirmed, "a clone doesn't share its lists")
	assert.Equal(t, []int64{3}, ev.Vetoed)
}

func TestGroupsDescribeEachMember(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		study := newKodi(t, fakekodi.Options{Library: testLibrary(30, 100, "2024-03-09 18:00:00")})
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Study", study, ""), kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, ""))

		settled(t, func() bool {
			g, _ := a.groupOf("Lounge")
			return len(g.Members) == 2 && a.fingerprintedAll("Study", "Lounge", "Bedroom")
		})

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
	})
}
