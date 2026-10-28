package api

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestNullableJSON(t *testing.T) {
	t.Parallel()

	type doc struct {
		User Nullable[User] `json:"user"`
	}

	b, err := json.Marshal(doc{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"user":null}`, string(b), "absent is null, not an empty object")

	b, err = json.Marshal(doc{User: Some(User{Name: "admin"})})
	require.NoError(t, err)
	assert.JSONEq(t, `{"user":{"name":"admin"}}`, string(b))

	var d doc

	require.NoError(t, json.Unmarshal([]byte(`{"user":{"name":"admin"}}`), &d))
	require.NotNil(t, d.User.V)
	assert.Equal(t, "admin", d.User.V.Name)

	require.NoError(t, json.Unmarshal([]byte(`{"user":null}`), &d))
	assert.Nil(t, d.User.V, "null clears a value")

	d = doc{User: Some(User{Name: "kept"})}
	require.NoError(t, json.Unmarshal([]byte(`{}`), &d))
	assert.Equal(t, "kept", d.User.V.Name, "an absent field is left alone")

	assert.Error(t, json.Unmarshal([]byte(`{"user":"admin"}`), &d), "a value of the wrong type is an error")
}

func TestPropNullableRoundTrip(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		var in Nullable[int64]
		if rapid.Bool().Draw(t, "set") {
			in = Some(rapid.Int64().Draw(t, "v"))
		}

		b, err := json.Marshal(in)
		require.NoError(t, err)

		var out Nullable[int64]
		require.NoError(t, json.Unmarshal(b, &out))
		assert.Equal(t, in, out)
	})
}

func TestNullableSchema(t *testing.T) {
	t.Parallel()

	r := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	s := Nullable[User]{}.Schema(r)

	require.Len(t, s.OneOf, 2, "the object or null")
	assert.Equal(t, "#/components/schemas/User", s.OneOf[0].Ref)
	assert.Equal(t, "null", s.OneOf[1].Type)
}
