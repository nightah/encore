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

	assert.Equal(t, "21.3", formatVersion(v(21, 3, "stable", "")))
	assert.Equal(t, "22.0-rc1", formatVersion(v(22, 0, "releasecandidate", "1")))
	assert.Equal(t, "22.0-beta2", formatVersion(v(22, 0, "beta", "2")))
	assert.Empty(t, formatVersion(nil))
}
