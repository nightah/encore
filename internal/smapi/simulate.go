package smapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

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
	return c.simulate(ctx, skillID, locale, text, smapiclient.V2SkillSimulationsSessionModeFORCENEWSESSION)
}

// SimulateInSession is Simulate in the session the last simulation left open, so a dialog's next turn reaches the skill as an answer. With no session open it starts one.
func (c *Client) SimulateInSession(ctx context.Context, skillID, locale, text string) (string, error) {
	return c.simulate(ctx, skillID, locale, text, smapiclient.V2SkillSimulationsSessionModeDEFAULT)
}

// Say simulates text, in the open session when inSession is set (see SimulateInSession), and waits for the outcome.
func (c *Client) Say(ctx context.Context, skillID, locale, text string, inSession bool) (*Simulation, error) {
	start := c.Simulate
	if inSession {
		start = c.SimulateInSession
	}

	var (
		id    string
		tries int
	)

	err := c.Poll(ctx, func(ctx context.Context) (bool, error) {
		tries++

		var err error

		id, err = start(ctx, skillID, locale, text)
		if busy(err) && tries < busyTries {
			return false, nil
		}

		return true, err
	})
	if err != nil {
		return nil, err
	}

	var sim *Simulation

	err = c.Poll(ctx, func(ctx context.Context) (bool, error) {
		sim, err = c.Simulation(ctx, skillID, id)
		if err != nil {
			return false, err
		}

		return sim.Status != SimulationInProgress, nil
	})

	return sim, err
}

func (c *Client) simulate(ctx context.Context, skillID, locale, text string, mode smapiclient.V2SkillSimulationsSessionMode) (string, error) {
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
				ConsideredIntents []simIntent `json:"consideredIntents"`
				AlexaResponses    []struct {
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

		if r.SkillExecutionInfo != nil {
			out.Invoked = len(r.SkillExecutionInfo.Invocations) > 0

			for _, inv := range r.SkillExecutionInfo.Invocations {
				if resp, ok := skillResponse(inv); ok {
					out.Responses = append(out.Responses, resp)
				}
			}
		}

		if r.AlexaExecutionInfo != nil {
			if in := r.AlexaExecutionInfo.ConsideredIntents; len(in) > 0 {
				out.Intent, out.Slots = in[0].Name, in[0].slots()
			}

			for _, ar := range r.AlexaExecutionInfo.AlexaResponses {
				if ar.Content != nil && ar.Content.Caption != "" {
					out.Captions = append(out.Captions, ar.Content.Caption)
				}
			}
		}
	}

	return out, nil
}

// skillResponse reads one invocation's request type and response. An invocation whose body isn't a skill response, as when the endpoint failed, is left out.
func skillResponse(raw json.RawMessage) (SkillResponse, bool) {
	var inv simInvocation
	if json.Unmarshal(raw, &inv) != nil || inv.InvocationResponse == nil {
		return SkillResponse{}, false
	}

	r := inv.InvocationResponse.Body.Response
	out := SkillResponse{Request: inv.InvocationRequest.Body.Request.Type, Speech: r.OutputSpeech.said(), EndSession: r.ShouldEndSession}

	if r.Reprompt != nil {
		out.Reprompt = r.Reprompt.OutputSpeech.said()
	}

	for _, d := range r.Directives {
		out.Directives = append(out.Directives, d.Type)
	}

	return out, true
}

func (s *simSpeech) said() string {
	if s == nil {
		return ""
	}

	return cmp.Or(s.Text, s.SSML)
}

func (i simIntent) slots() map[string]SimulationSlot {
	var out map[string]SimulationSlot

	for name, sl := range i.Slots {
		if sl.Value == "" {
			continue
		}

		v := SimulationSlot{Value: sl.Value}

		if sl.Resolutions != nil {
			for _, a := range sl.Resolutions.PerAuthority {
				if a.Status.Code == resolutionMatch && len(a.Values) > 0 {
					v.Resolved = a.Values[0].Value.Name

					break
				}
			}
		}

		if out == nil {
			out = map[string]SimulationSlot{}
		}

		out[name] = v
	}

	return out
}

// busy reports whether err is the simulator turning a simulation down because the skill's last one is still being processed, as it does for a moment after that one's result is in.
func busy(err error) bool {
	var e *Error

	return errors.As(err, &e) && e.StatusCode == http.StatusConflict && strings.Contains(e.Message, busyMessage)
}
