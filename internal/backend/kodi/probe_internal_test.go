package kodi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

func TestFormatVersion(t *testing.T) {
	v := func(major, minor int, tag, n string) *kodirpc.ApplicationPropertyValueVersion {
		return &kodirpc.ApplicationPropertyValueVersion{Major: major, Minor: minor, Tag: kodirpc.ApplicationPropertyValueVersionTag(tag), Tagversion: n}
	}

	testCases := []struct {
		name     string
		have     *kodirpc.ApplicationPropertyValueVersion
		expected string
	}{
		{name: "ShouldFormatStable", have: v(21, 3, "stable", ""), expected: "21.3"},
		{name: "ShouldFormatReleaseCandidate", have: v(22, 0, "releasecandidate", "1"), expected: "22.0-rc1"},
		{name: "ShouldFormatBeta", have: v(22, 0, "beta", "2"), expected: "22.0-beta2"},
		{name: "ShouldFormatNilAsEmpty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatVersion(tc.have))
		})
	}
}
