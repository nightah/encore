package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	out, _, err := runCLI(t, "version")
	require.NoError(t, err)
	assert.Equal(t, buildinfo.Get().String()+"\n", out)
}

func TestVersionCommandJSON(t *testing.T) {
	out, _, err := runCLI(t, "version", "--json")
	require.NoError(t, err)

	var got buildinfo.Info
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, buildinfo.Get(), got)
	assert.Contains(t, out, "\n  \"version\": ", "indented for people to read")
}

func TestVersionCommandTakesNoArguments(t *testing.T) {
	_, _, err := runCLI(t, "version", "extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}
