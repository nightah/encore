package smapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkillResponseSkipsInvocationsWithoutAResponse(t *testing.T) {
	for name, raw := range map[string]string{
		"no response":   `{"invocationRequest": {"body": {"request": {"type": "IntentRequest"}}}}`,
		"not an object": `"the endpoint timed out"`,
	} {
		_, ok := skillResponse(json.RawMessage(raw))
		assert.False(t, ok, name)
	}

	r, ok := skillResponse(json.RawMessage(`{"invocationResponse": {"body": {"response": {"outputSpeech": {"type": "SSML", "ssml": "<speak>Hi</speak>"}}}}}`))
	assert.True(t, ok)
	assert.Equal(t, SkillResponse{Speech: "<speak>Hi</speak>"}, r, "SSML stands in for missing text")
}
