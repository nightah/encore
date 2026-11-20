package smapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkillResponseSkipsInvocationsWithoutAResponse(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected SkillResponse
		ok       bool
	}{
		{name: "ShouldSkipInvocationWithoutResponse", have: `{"invocationRequest": {"body": {"request": {"type": "IntentRequest"}}}}`},
		{name: "ShouldSkipInvocationThatIsNotAnObject", have: `"the endpoint timed out"`},
		{name: "ShouldUseSSMLWhenTextIsMissing", have: `{"invocationResponse": {"body": {"response": {"outputSpeech": {"type": "SSML", "ssml": "<speak>Hi</speak>"}}}}}`, expected: SkillResponse{Speech: "<speak>Hi</speak>"}, ok: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := skillResponse(json.RawMessage(tc.have))

			assert.Equal(t, tc.ok, ok)

			if tc.ok {
				assert.Equal(t, tc.expected, actual)
			}
		})
	}
}
