package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillEmpty(t *testing.T) {
	type inner struct {
		Tags []string          `json:"tags"`
		Meta map[string]string `json:"meta"`
	}

	type outer struct {
		Items  []inner          `json:"items"`
		Ptr    *inner           `json:"ptr"`
		Nil    *inner           `json:"nil"`
		Opt    Nullable[inner]  `json:"opt"`
		Raw    []byte           `json:"raw"`
		ByName map[string]inner `json:"by_name"`
	}

	in := outer{Items: []inner{{}}, Ptr: &inner{}, Opt: Some(inner{}), ByName: map[string]inner{"a": {}}}

	b, err := json.Marshal(fillEmpty(in))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"tags":[],"meta":{}}],"ptr":{"tags":[],"meta":{}},"nil":null,"opt":{"tags":[],"meta":{}},"raw":null,"by_name":{"a":{"tags":[],"meta":{}}}}`, string(b))

	// The original is untouched.
	assert.Nil(t, in.Items[0].Tags)
	assert.Nil(t, in.Ptr.Tags)

	var none []inner

	b, err = json.Marshal(fillEmpty(none))
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(b))

	assert.Equal(t, 3, fillEmpty(3))
	assert.Nil(t, fillEmpty(nil))
}
