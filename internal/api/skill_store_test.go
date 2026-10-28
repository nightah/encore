package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestSecretTokenStore(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	s := secretTokenStore{sec: h.app.Secrets()}

	tok, err := s.Load(t.Context())
	require.NoError(t, err, "nothing stored is not an error")
	assert.Nil(t, tok)

	expiry := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	require.NoError(t, s.Save(t.Context(), &oauth2.Token{AccessToken: "Atza|access", RefreshToken: "Atzr|refresh", TokenType: "bearer", Expiry: expiry}))

	tok, err = s.Load(t.Context())
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "Atza|access", tok.AccessToken)
	assert.Equal(t, "Atzr|refresh", tok.RefreshToken)
	assert.True(t, expiry.Equal(tok.Expiry))

	raw, err := h.app.Secrets().Get(t.Context(), lwaTokenRef)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Atzr|refresh", "stored through the encrypted secrets store")

	require.NoError(t, s.forget(t.Context()))

	tok, err = s.Load(t.Context())
	require.NoError(t, err)
	assert.Nil(t, tok, "forgotten")

	require.NoError(t, h.app.Secrets().Put(t.Context(), lwaTokenRef, []byte("{not json")))

	_, err = s.Load(t.Context())
	require.Error(t, err, "a damaged token is an error, not a missing one")
}
