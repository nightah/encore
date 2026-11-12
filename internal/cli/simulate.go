package cli

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"

	"github.com/nightah/encore/internal/api"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/store"
)

func newGenSimulateCommand() *cobra.Command {
	var (
		sf        smapiFlags
		locale    string
		file      string
		inSession bool
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "simulate [utterance...]",
		Short: "Say utterances to Amazon's simulator against a deployed skill, and show or check what Alexa heard and the skill answered",
		Long: "Says each utterance to the simulation API in turn, as typed text, and reports the intent and slots Alexa read, whether the skill was reached and what it and Alexa said. Include the invocation, as in “ask encore to switch to abc news”.\n" +
			"With --file, the utterances come from a YAML file of cases, each of which may expect an intent, slots and a regular expression Alexa's reply must match, and the command fails when any case doesn't.\n" +
			"The simulator sends the skill's endpoint real requests: against a skill Encore deployed, the utterances really control your Kodi instances. The skill must be enabled for testing; one that isn't is enabled while the utterances are said and disabled again after.\n" +
			"By default the skill is the one Encore deployed, reached with the Login with Amazon connection Encore keeps in its data directory, in its default locale; its database is only read, so this can run beside the server. --skill names another, reached with " + envClientID + ", " + envClientSecret + " and " + envRefreshToken + " when they're set, else with Encore's connection. With no data directory to read, the skill " + envSkillID + " names is used with those variables.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cases, err := simulationCases(file, args)
			if err != nil {
				return err
			}

			c, skill, dirLocale, err := sf.simulationTarget(cmd)
			if err != nil {
				return err
			}

			fallback := cmp.Or(dirLocale, simulateLocale)

			log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelDebug}))

			restore, err := enableForCheck(cmd.Context(), c, skill, log)
			if err != nil {
				return err
			}

			run := simulationRun{w: cmd.OutOrStdout(), json: asJSON}

			err = func() error {
				for i, sc := range cases.Cases {
					loc := cases.locale(sc, locale, fallback)

					sim, err := c.Say(cmd.Context(), skill, loc, sc.Utterance, inSession && i > 0)
					if err != nil {
						return fmt.Errorf("%s: %w", quote(sc.Utterance), err)
					}

					if err := run.report(loc, sc, sim, file != ""); err != nil {
						return err
					}
				}

				return nil
			}()
			if err := errors.Join(err, restore()); err != nil {
				return err
			}

			return run.finish(file != "")
		},
	}

	sf.register(cmd)
	cmd.Flags().StringVarP(&locale, "locale", "l", "", "the locale to say the utterances in, overriding a file's own (default the data directory's default locale, else "+simulateLocale+")")
	cmd.Flags().StringVarP(&file, "file", "f", "", "a YAML file of cases to say and check, instead of utterances")
	cmd.Flags().BoolVar(&inSession, "session", false, "say every utterance after the first in the session the one before left open, as a dialogue's answers")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print each result as a line of JSON")

	return cmd
}

// simulationCases reads the cases to say from file, or makes one case of each utterance.
func simulationCases(file string, utterances []string) (simCases, error) {
	switch {
	case file != "" && len(utterances) > 0:
		return simCases{}, errors.New("give utterances or --file, not both")
	case file == "" && len(utterances) == 0:
		return simCases{}, errors.New("give an utterance to say, or --file")
	case file == "":
		var cs simCases
		for _, u := range utterances {
			cs.Cases = append(cs.Cases, simCase{Utterance: u})
		}

		return cs, nil
	}

	b, err := os.ReadFile(file) //nolint:gosec // the developer names the cases file to read
	if err != nil {
		return simCases{}, err
	}

	var cs simCases

	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)

	if err := dec.Decode(&cs); err != nil {
		return simCases{}, fmt.Errorf("%s: %w", file, err)
	}

	if len(cs.Cases) == 0 {
		return simCases{}, fmt.Errorf("%s: no cases", file)
	}

	for i := range cs.Cases {
		sc := &cs.Cases[i]
		if strings.TrimSpace(sc.Utterance) == "" {
			return simCases{}, fmt.Errorf("%s: case %d has no utterance", file, i+1)
		}

		if sc.Says != "" {
			if sc.says, err = regexp.Compile(sc.Says); err != nil {
				return simCases{}, fmt.Errorf("%s: %s: says: %w", file, quote(sc.Utterance), err)
			}
		}
	}

	return cs, nil
}

// locale is the locale to say sc in: flag when it's set, else the case's, else the file's, else fallback.
func (cs simCases) locale(sc simCase, flag, fallback string) string {
	return cmp.Or(flag, sc.Locale, cs.Locale, fallback)
}

// simulationTarget picks the skill to simulate, a client to reach it with and a default locale. A skill named with --skill is reached with the SMAPI_* credentials when they're set, else with those Encore keeps in its data directory, and takes the data directory's locale only when it is the skill stored there. With no --skill, the skill Encore deployed is used, with its credentials and default locale from the data directory, and failing that the skill SMAPI_SKILL_ID names with the SMAPI_* credentials.
func (f *smapiFlags) simulationTarget(cmd *cobra.Command) (*smapi.Client, string, string, error) {
	env := os.Getenv(envClientID) != "" && os.Getenv(envClientSecret) != "" && os.Getenv(envRefreshToken) != ""

	if f.skill != "" && env {
		c, skill, err := f.client()

		return c, skill, "", err
	}

	access, dirErr := dataDirAccess(cmd)

	switch {
	case dirErr == nil:
		c, err := f.clientWith(access.ClientID, access.ClientSecret, access.RefreshToken)

		// The stored locale is the stored skill's; another takes the default.
		if f.skill != "" && f.skill != access.SkillID {
			return c, f.skill, "", err
		}

		return c, access.SkillID, access.Locale, err
	case f.skill == "" && env && os.Getenv(envSkillID) != "":
		c, skill, err := f.client()

		return c, skill, "", err
	}

	return nil, "", "", fmt.Errorf("no skill to simulate: %w; deploy one from Encore's Alexa skill page, or set %s, %s, %s and --skill or %s", dirErr, envClientID, envClientSecret, envRefreshToken, envSkillID)
}

// dataDirAccess reads the skill Encore deployed, and its credentials, from the configured data directory, without changing it.
func dataDirAccess(cmd *cobra.Command) (api.SkillAccess, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return api.SkillAccess{}, err
	}

	if cfg.Stateless {
		return api.SkillAccess{}, errors.New("a stateless Encore keeps no data directory to read")
	}

	db, err := store.OpenReadOnly(cmd.Context(), store.DefaultPath(cfg.DataDir))
	if err != nil {
		return api.SkillAccess{}, err
	}
	defer db.Close()

	key, err := secrets.ReadKey(secrets.Options{Key: cfg.Secrets.Key, KeyFile: cfg.Secrets.KeyFile, DataDir: cfg.DataDir})
	if err != nil {
		return api.SkillAccess{}, err
	}

	sec, err := secrets.OpenWithKey(db.SecretRows(), key)
	if err != nil {
		return api.SkillAccess{}, err
	}

	return api.ReadSkillAccess(cmd.Context(), cfg, db, sec)
}

// misses lists how sim falls short of what sc expects.
func (sc simCase) misses(sim *smapi.Simulation) []string {
	var out []string

	if sc.Intent != "" {
		switch {
		case sim.Status != smapi.SimulationSuccessful:
			out = append(out, fmt.Sprintf("want intent %s; the utterance resolved to none of the skill's intents (%s)", sc.Intent, heard(sim)))
		case sim.Intent != sc.Intent:
			out = append(out, fmt.Sprintf("want intent %s, got %s", sc.Intent, heard(sim)))
		}
	}

	for _, name := range slices.Sorted(maps.Keys(sc.Slots)) {
		want := sc.Slots[name]

		got, ok := sim.Slots[name]

		switch {
		case !ok:
			out = append(out, fmt.Sprintf("want slot %s filled, but it's empty", name))
		case want != "" && !strings.EqualFold(want, got.Value) && !strings.EqualFold(want, got.Resolved):
			out = append(out, fmt.Sprintf("want slot %s=%q, got %s", name, want, slotText(got)))
		}
	}

	if sc.says != nil {
		if said := reply(sim); !sc.says.MatchString(said) {
			out = append(out, fmt.Sprintf("want a reply matching %s, got %s", quote(sc.Says), quote(said)))
		}
	}

	return out
}

// report writes one utterance's outcome, checked against sc when check is set.
func (r *simulationRun) report(locale string, sc simCase, sim *smapi.Simulation, check bool) error {
	var misses []string
	if check {
		misses = sc.misses(sim)
		if len(misses) > 0 {
			r.failed++
		} else {
			r.passed++
		}
	}

	if r.json {
		return json.NewEncoder(r.w).Encode(simResult{Locale: locale, Utterance: sc.Utterance, Simulation: sim, Misses: misses})
	}

	var b strings.Builder

	switch {
	case !check:
		fmt.Fprintf(&b, "%s\n", quote(sc.Utterance))
	case len(misses) > 0:
		fmt.Fprintf(&b, "FAIL  %s\n", quote(sc.Utterance))
	default:
		fmt.Fprintf(&b, "PASS  %s\n", quote(sc.Utterance))
	}

	for _, m := range misses {
		fmt.Fprintf(&b, "  %s\n", m)
	}

	if !check || len(misses) > 0 {
		writeSimulation(&b, sim)
	}

	_, err := io.WriteString(r.w, b.String())

	return err
}

// finish writes a check's tally and fails when a case failed.
func (r *simulationRun) finish(check bool) error {
	if !check {
		return nil
	}

	if !r.json {
		if _, err := fmt.Fprintf(r.w, "%d passed, %d failed\n", r.passed, r.failed); err != nil {
			return err
		}
	}

	if r.failed > 0 {
		return errSilent
	}

	return nil
}

func writeSimulation(b *strings.Builder, sim *smapi.Simulation) {
	if sim.Status == smapi.SimulationSuccessful {
		fmt.Fprintf(b, "  intent:  %s\n", heard(sim))
	} else {
		fmt.Fprintf(b, "  failed:  %s\n", sim.Error)

		if sim.Intent != "" {
			fmt.Fprintf(b, "  weighed: %s\n", heard(sim))
		}
	}

	for _, resp := range sim.Responses {
		state := "reached"

		switch {
		case resp.EndSession == nil:
			state += ", session left as it was"
		case *resp.EndSession:
			state += ", session ended"
		default:
			state += ", session open"
		}

		if len(resp.Directives) > 0 {
			state += ", directives " + strings.Join(resp.Directives, ", ")
		}

		fmt.Fprintf(b, "  skill:   %s\n", state)

		if resp.Reprompt != "" {
			fmt.Fprintf(b, "  reprompt: %s\n", quote(resp.Reprompt))
		}
	}

	switch {
	case len(sim.Responses) > 0:
	case sim.Invoked:
		// The endpoint answered with something the simulator didn't pass on as a skill response, such as an error page.
		fmt.Fprintf(b, "  skill:   reached, but its response couldn't be read\n")
	case sim.Status == smapi.SimulationSuccessful:
		fmt.Fprintf(b, "  skill:   not reached\n")
	}

	if said := reply(sim); said != "" {
		fmt.Fprintf(b, "  Alexa:   %s\n", quote(said))
	}
}

// heard is the intent Alexa weighed first, with its slots.
func heard(sim *smapi.Simulation) string {
	if sim.Intent == "" {
		return "no intent"
	}

	parts := []string{sim.Intent}
	for _, name := range slices.Sorted(maps.Keys(sim.Slots)) {
		parts = append(parts, name+"="+slotText(sim.Slots[name]))
	}

	return strings.Join(parts, " ")
}

func slotText(s smapi.SimulationSlot) string {
	if s.Resolved != "" && !strings.EqualFold(s.Resolved, s.Value) {
		return fmt.Sprintf("%q (%s)", s.Value, s.Resolved)
	}

	return fmt.Sprintf("%q", s.Value)
}

// reply is what Alexa said: its captions, or the skill's speech when it captioned none.
func reply(sim *smapi.Simulation) string {
	if len(sim.Captions) > 0 {
		return strings.Join(sim.Captions, " ")
	}

	var said []string

	for _, r := range sim.Responses {
		if r.Speech != "" {
			said = append(said, r.Speech)
		}
	}

	return strings.Join(said, " ")
}
