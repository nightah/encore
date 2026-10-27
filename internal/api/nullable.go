package api

import (
	"encoding/json"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// Some wraps v.
func Some[T any](v T) Nullable[T] { return Nullable[T]{V: &v} }

// MarshalJSON implements json.Marshaler.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.V == nil {
		return []byte("null"), nil
	}

	return json.Marshal(n.V)
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.V = nil

		return nil
	}

	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	n.V = &v

	return nil
}

// Schema implements huma.SchemaProvider.
func (Nullable[T]) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{r.Schema(reflect.TypeFor[T](), true, ""), {Type: "null"}}}
}
