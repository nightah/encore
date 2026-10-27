package smapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// UnmarshalJSON implements json.Unmarshaler.
func (t *Time) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		t.Time = time.Time{}

		return nil
	}

	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}

	if s == "" {
		t.Time = time.Time{}

		return nil
	}

	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		t.Time = time.UnixMilli(ms).UTC()

		return nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("smapi: timestamp %q is neither RFC 3339 nor Unix milliseconds", s)
	}

	t.Time = parsed

	return nil
}

// MarshalJSON writes RFC 3339, as the spec documents.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(t.Format(time.RFC3339Nano))
}
