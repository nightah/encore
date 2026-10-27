//go:build smapi_live

package smapi_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/lwa"
)

// liveClient authorises against the real SMAPI with a maintainer's security profile and refresh token. The tests are read-only.
func liveClient(t *testing.T) *smapi.Client {
	t.Helper()

	id, secret, refresh := os.Getenv("SMAPI_CLIENT_ID"), os.Getenv("SMAPI_CLIENT_SECRET"), os.Getenv("SMAPI_REFRESH_TOKEN")
	if id == "" || secret == "" || refresh == "" {
		t.Skip("SMAPI_CLIENT_ID, SMAPI_CLIENT_SECRET and SMAPI_REFRESH_TOKEN must be set")
	}

	lc, err := lwa.New(lwa.Config{Credentials: lwa.Credentials{ClientID: id, ClientSecret: secret}})
	require.NoError(t, err)

	store := &lwa.MemoryStore{}
	require.NoError(t, store.Save(t.Context(), &oauth2.Token{RefreshToken: refresh}))

	c, err := smapi.New(lc.TokenSource(store), smapi.Options{})
	require.NoError(t, err)

	return c
}

func TestLiveVendorsAndSkills(t *testing.T) {
	c := liveClient(t)

	vendors, err := c.Vendors(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, vendors, "the account has no developer vendor")

	vendorID := os.Getenv("SMAPI_VENDOR_ID")
	if vendorID == "" {
		vendorID = vendors[0].ID
	}

	t.Logf("%d vendors; listing skills for the chosen one", len(vendors))

	skills, err := c.ListSkills(t.Context(), vendorID)
	require.NoError(t, err)

	for _, s := range skills {
		require.NotEmpty(t, s.ID)
	}

	t.Logf("%d skills", len(skills))
}
