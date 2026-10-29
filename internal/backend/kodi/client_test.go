package kodi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

func TestSupports(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	c := NewClient(testConfig(t, s), nil)

	assert.Same(t, c.t, c.Transport())
	assert.True(t, c.Supports(t.Context(), "Addons.Unheard"), "a method Encore doesn't know is assumed to exist")
	assert.Empty(t, s.CallsTo(kodirpc.MethodJSONRPCVersion), "without asking")
	assert.False(t, c.Supports(t.Context(), kodirpc.MethodPlayerGetChapters), "Kodi 21 lacks chapters")

	failing := fakekodi.New(t, fakekodi.Options{})
	failWith(failing, kodirpc.MethodJSONRPCVersion)
	c = NewClient(testConfig(t, failing), nil)

	assert.True(t, c.Supports(t.Context(), kodirpc.MethodPlayerGetChapters), "an unknown version lets the call itself decide")
}
