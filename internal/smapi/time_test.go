package smapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeUnmarshal(t *testing.T) {
	want := time.Date(2026, 10, 26, 7, 38, 45, 519_000_000, time.UTC)

	for name, raw := range map[string]string{
		"milliseconds as a string, as the live API sends expiresAt": `"1793000325519"`,
		"milliseconds as a number":                                  `1793000325519`,
		"RFC 3339, as the spec documents":                           `"2026-10-26T07:38:45.519Z"`,
		"RFC 3339 with an offset":                                   `"2026-10-26T18:38:45.519+11:00"`,
	} {
		t.Run(name, func(t *testing.T) {
			var got Time
			require.NoError(t, json.Unmarshal([]byte(raw), &got))
			assert.True(t, want.Equal(got.Time), "got %s", got.Time)
		})
	}

	for _, raw := range []string{`null`, `""`} {
		var got Time
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.True(t, got.IsZero(), raw)
	}

	var bad Time
	assert.ErrorContains(t, json.Unmarshal([]byte(`"next tuesday"`), &bad), "neither RFC 3339 nor Unix milliseconds")
}

func TestUploadDecodesLiveResponse(t *testing.T) {
	var u Upload
	require.NoError(t, json.Unmarshal([]byte(`{"uploadUrl":"https://s3.example/upload","expiresAt":"1793002725519"}`), &u))
	assert.Equal(t, "https://s3.example/upload", u.URL)
	assert.Equal(t, int64(1793002725519), u.ExpiresAt.UnixMilli())

	out, err := json.Marshal(u)
	require.NoError(t, err)
	assert.JSONEq(t, `{"uploadUrl":"https://s3.example/upload","expiresAt":"2026-10-26T08:18:45.519Z"}`, string(out))
}
