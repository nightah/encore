package smapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeUnmarshal(t *testing.T) {
	expected := time.Date(2026, 10, 26, 7, 38, 45, 519_000_000, time.UTC)

	testCases := []struct {
		name     string
		have     string
		expected time.Time
		err      string
	}{
		{name: "ShouldUnmarshalMillisecondsStringAsTheLiveAPISendsExpiresAt", have: `"1793000325519"`, expected: expected},
		{name: "ShouldUnmarshalMillisecondsNumber", have: `1793000325519`, expected: expected},
		{name: "ShouldUnmarshalRFC3339AsTheSpecDocuments", have: `"2026-10-26T07:38:45.519Z"`, expected: expected},
		{name: "ShouldUnmarshalRFC3339WithOffset", have: `"2026-10-26T18:38:45.519+11:00"`, expected: expected},
		{name: "ShouldUnmarshalNullAsZero", have: `null`},
		{name: "ShouldUnmarshalEmptyStringAsZero", have: `""`},
		{name: "ShouldRejectUnknownFormat", have: `"next tuesday"`, err: "neither RFC 3339 nor Unix milliseconds"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var actual Time

			err := json.Unmarshal([]byte(tc.have), &actual)

			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.True(t, tc.expected.Equal(actual.Time), "got %s", actual.Time)
		})
	}
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

func TestTimeMarshalZeroIsNull(t *testing.T) {
	out, err := json.Marshal(Time{})
	require.NoError(t, err)
	assert.JSONEq(t, `null`, string(out))

	melbourne, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	out, err = json.Marshal(Time{time.Date(2026, 10, 28, 9, 0, 0, 0, melbourne)})
	require.NoError(t, err)
	assert.JSONEq(t, `"2026-10-28T09:00:00+11:00"`, string(out))
}

func TestTimeUnmarshalRejectsMalformedString(t *testing.T) {
	var got Time
	require.Error(t, got.UnmarshalJSON([]byte(`"unterminated`)))
}
