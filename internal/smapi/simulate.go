package smapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/nightah/encore/internal/smapi/smapiclient"
)

// SimulationLocales are the locales the simulation API supports.
var SimulationLocales = []string{"de-DE", "en-AU", "en-CA", "en-GB", "en-IN", "en-US", "fr-FR", "ja-JP"}

// CanSimulate reports whether the simulation API supports locale.
func CanSimulate(locale string) bool {
	return slices.Contains(SimulationLocales, locale)
}

// Simulate sends text as if spoken to a device in locale, in a new session, and returns the simulation ID. The skill must be enabled; only one simulation may run per skill at a time.
func (c *Client) Simulate(ctx context.Context, skillID, locale, text string) (string, error) {
	mode := smapiclient.V2SkillSimulationsSessionModeFORCENEWSESSION
	call := func() (*http.Response, error) {
		return c.api.SimulateSkillV2(ctx, skillID, StageDevelopment, smapiclient.SimulateSkillV2JSONRequestBody{
			Device:  smapiclient.V2SkillSimulationsDevice{Locale: locale},
			Input:   smapiclient.V2SkillSimulationsInput{Content: text},
			Session: &smapiclient.V2SkillSimulationsSession{Mode: &mode},
		})
	}

	var out struct {
		ID string `json:"id"`
	}

	if err := decode("simulate", call, &out); err != nil {
		return "", err
	}

	return out.ID, nil
}

// Simulation returns the state of a simulation.
func (c *Client) Simulation(ctx context.Context, skillID, simulationID string) (*Simulation, error) {
	call := func() (*http.Response, error) {
		return c.api.GetSkillSimulationV2(ctx, skillID, StageDevelopment, simulationID)
	}

	// Invocation bodies mix strings and objects, which the generated types cannot hold, so they are left raw.
	var raw struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Result *struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			SkillExecutionInfo *struct {
				Invocations []json.RawMessage `json:"invocations"`
			} `json:"skillExecutionInfo"`
			AlexaExecutionInfo *struct {
				AlexaResponses []struct {
					Content *struct {
						Caption string `json:"caption"`
					} `json:"content"`
				} `json:"alexaResponses"`
			} `json:"alexaExecutionInfo"`
		} `json:"result"`
	}

	if err := decode("get simulation", call, &raw); err != nil {
		return nil, err
	}

	out := &Simulation{ID: raw.ID, Status: raw.Status}

	if r := raw.Result; r != nil {
		if r.Error != nil {
			out.Error = r.Error.Message
		}

		out.Invoked = r.SkillExecutionInfo != nil && len(r.SkillExecutionInfo.Invocations) > 0

		if r.AlexaExecutionInfo != nil {
			for _, ar := range r.AlexaExecutionInfo.AlexaResponses {
				if ar.Content != nil && ar.Content.Caption != "" {
					out.Captions = append(out.Captions, ar.Content.Caption)
				}
			}
		}
	}

	return out, nil
}
