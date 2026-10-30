package smapi

import (
	"context"
	"net/http"

	"github.com/nightah/encore/internal/smapi/smapiclient"
)

// ProfileNLU asks how Alexa's NLU reads utterance against locale's built interaction model in the development stage, without invoking the skill. Unlike the simulation API it works in every locale and needs no enablement. Amazon answers it synchronously but now and then with a 500, which the client retries.
func (c *Client) ProfileNLU(ctx context.Context, skillID, locale, utterance string) (*NLUProfile, error) {
	var out NLUProfile

	if err := decode("profile utterance", func() (*http.Response, error) {
		return c.api.ProfileNluV1(ctx, skillID, StageDevelopment, locale, smapiclient.ProfileNluV1JSONRequestBody{Utterance: utterance})
	}, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// Intent returns the selected intent's name, or "" when NLU selected none.
func (p *NLUProfile) Intent() string {
	if p == nil || p.Selected == nil {
		return ""
	}

	return p.Selected.Name
}

// Resolved returns the names and IDs entity resolution matched the slot's words to, across every authority.
func (s NLUSlot) Resolved() []string {
	if s.Resolutions == nil {
		return nil
	}

	var out []string

	for _, a := range s.Resolutions.PerAuthority {
		for _, v := range a.Values {
			// Amazon's spec puts name and id on the item; live responses, like a skill request's, wrap them in value.
			name, id := v.Name, v.ID
			if v.Value != nil {
				name, id = v.Value.Name, v.Value.ID
			}

			for _, s := range []string{name, id} {
				if s != "" {
					out = append(out, s)
				}
			}
		}
	}

	return out
}
