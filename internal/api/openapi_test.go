package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite openapi.json from the registered operations")

// TestOpenAPIDrift keeps the committed openapi.json, which the web client is generated from, in step with the code. Run `go generate ./internal/api` after changing an operation.
func TestOpenAPIDrift(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})

	got, err := json.MarshalIndent(h.api.OpenAPI(), "", "  ")
	require.NoError(t, err)

	got = append(got, '\n')

	if *update {
		require.NoError(t, os.WriteFile("openapi.json", got, 0o644))

		return
	}

	want, err := os.ReadFile("openapi.json")
	require.NoError(t, err)

	if !bytes.Equal(want, got) {
		t.Fatal("openapi.json is out of date; run `go generate ./internal/api` and commit the result")
	}

	served := h.client().do("GET", "/api/v1/openapi.json", nil)
	require.Equal(t, 200, served.status)

	var a, b map[string]any

	require.NoError(t, json.Unmarshal(served.body, &a))
	require.NoError(t, json.Unmarshal(want, &b))
	assert.Equal(t, b, a, "the served document matches the committed one")
}
