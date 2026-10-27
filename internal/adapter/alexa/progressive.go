package alexa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NewProgressive returns a sender using client, or http.DefaultClient when nil.
func NewProgressive(client *http.Client) *Progressive {
	if client == nil {
		client = http.DefaultClient
	}

	return &Progressive{client: client}
}

// Send speaks text on the device that made req. It is best effort: Alexa limits progressive responses to a few per request and they are only a courtesy.
func (p *Progressive) Send(ctx context.Context, env *RequestEnvelope, text string) error {
	sys := env.Context.System
	if sys.APIEndpoint == "" || sys.APIAccessToken == "" || text == "" {
		return nil
	}

	body, err := json.Marshal(map[string]any{
		"header":    map[string]string{"requestId": env.Request.RequestID},
		"directive": map[string]string{"type": "VoicePlayer.Speak", "speech": text},
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, progressiveTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sys.APIEndpoint+"/v1/directives", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+sys.APIAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending progressive response: %w", err)
	}

	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sending progressive response: status %d", resp.StatusCode)
	}

	return nil
}
