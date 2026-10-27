package kodirpc

import (
	"bytes"
	"encoding/json"
)

func newUnion(v any) union { return union{val: v} }

// MarshalJSON implements json.Marshaler.
func (u union) MarshalJSON() ([]byte, error) {
	switch {
	case u.val != nil:
		return json.Marshal(u.val)
	case u.raw != nil:
		return u.raw, nil
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (u *union) UnmarshalJSON(b []byte) error {
	u.val = nil
	u.raw = append(u.raw[:0], b...)
	return nil
}

// IsZero reports whether no value is set, which lets omitzero drop unset optional unions.
func (u union) IsZero() bool { return u.val == nil && u.raw == nil }

// IsNull reports whether the value is unset or JSON null.
func (u union) IsNull() bool {
	return u.val == nil && (u.raw == nil || bytes.Equal(bytes.TrimSpace(u.raw), []byte("null")))
}

// Raw returns the JSON encoding of the value.
func (u union) Raw() (json.RawMessage, error) { return u.MarshalJSON() }

func unionAs[T any](u union) (T, error) {
	if v, ok := u.val.(T); ok {
		return v, nil
	}
	var t T
	b, err := u.MarshalJSON()
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(b, &t)
	return t, err
}
