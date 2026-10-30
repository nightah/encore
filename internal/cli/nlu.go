package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/nlu"
)

func (f *smapiFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.skill, "skill", "", "the deployed skill's ID (env "+envSkillID+")")
	cmd.Flags().StringVar(&f.smapiURL, "smapi-url", smapi.BaseURL, "SMAPI's base URL")
	cmd.Flags().StringVar(&f.tokenURL, "token-url", "", "Login with Amazon's token URL")
	_ = cmd.Flags().MarkHidden("smapi-url")
	_ = cmd.Flags().MarkHidden("token-url")
}

// client authorises with a security profile and refresh token from the environment, as the live SMAPI tests do, and returns the skill to work on.
func (f *smapiFlags) client() (*smapi.Client, string, error) {
	id, secret, refresh := os.Getenv(envClientID), os.Getenv(envClientSecret), os.Getenv(envRefreshToken)
	if id == "" || secret == "" || refresh == "" {
		return nil, "", fmt.Errorf("set %s, %s and %s to a security profile's client ID and secret and a refresh token it issued (see the development docs)", envClientID, envClientSecret, envRefreshToken)
	}

	skill := cmp.Or(f.skill, os.Getenv(envSkillID))
	if skill == "" {
		return nil, "", fmt.Errorf("name the skill with --skill or %s; the Alexa skill page in Encore shows its ID", envSkillID)
	}

	cfg := lwa.Config{Credentials: lwa.Credentials{ClientID: id, ClientSecret: secret}}
	if f.tokenURL != "" {
		cfg.Endpoint = &oauth2.Endpoint{TokenURL: f.tokenURL}
	}

	lc, err := lwa.New(cfg)
	if err != nil {
		return nil, "", err
	}

	store := &lwa.MemoryStore{}
	// MemoryStore.Save doesn't fail.
	_ = store.Save(context.Background(), &oauth2.Token{RefreshToken: refresh})

	c, err := smapi.New(lc.TokenSource(store), smapi.Options{BaseURL: f.smapiURL})

	return c, skill, err
}

func newGenNLUCommand() *cobra.Command {
	var (
		sf       smapiFlags
		locales  []string
		dir      string
		parallel int
		verbose  bool
	)

	cmd := &cobra.Command{
		Use:   "nlu",
		Short: "Check how Alexa's NLU reads the fixture utterances, against a deployed skill",
		Long: "Profiles each row of the NLU fixtures (one <locale>.yaml per locale) with SMAPI's profileNlu against the skill's development models, and reports each row whose intent or slots differ, with the intents Alexa weighed instead.\n" +
			"Pending rows are reported but don't fail the run until they pass. Deploy first: profileNlu reads the built models.\n" +
			"Credentials come from " + envClientID + ", " + envClientSecret + " and " + envRefreshToken + ".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fixtures, err := nlu.Load(os.DirFS(dir))
			if err != nil {
				return err
			}

			if len(fixtures) == 0 {
				return fmt.Errorf("no NLU fixtures in %s; run from the repository root or pass --fixtures", dir)
			}

			if fixtures, err = chooseFixtures(fixtures, locales); err != nil {
				return err
			}

			src, err := skillmodel.Load(i18n.FS)
			if err != nil {
				return err
			}

			for _, f := range fixtures {
				if err := f.Check(src); err != nil {
					return err
				}
			}

			c, skill, err := sf.client()
			if err != nil {
				return err
			}

			r := &nlu.Runner{Client: c, SkillID: skill, Parallel: parallel}

			var all []nlu.Result

			for _, f := range fixtures {
				res, err := r.Run(cmd.Context(), f)
				if err != nil {
					return err
				}

				all = append(all, res...)
			}

			if err := nlu.Write(cmd.OutOrStdout(), all, verbose); err != nil {
				return err
			}

			if nlu.Summarise(all).Failed() {
				return errSilent
			}

			return nil
		},
	}

	sf.register(cmd)
	cmd.Flags().StringSliceVarP(&locales, "locale", "l", nil, "locales to check (default every fixture)")
	cmd.Flags().StringVar(&dir, "fixtures", defaultNLUFixtures, "directory of <locale>.yaml fixtures")
	cmd.Flags().IntVar(&parallel, "parallel", 4, "utterances to profile at once")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "list the rows that pass too")

	return cmd
}

func chooseFixtures(all []nlu.Fixture, locales []string) ([]nlu.Fixture, error) {
	if len(locales) == 0 {
		return all, nil
	}

	var out []nlu.Fixture

	for _, loc := range locales {
		i := slices.IndexFunc(all, func(f nlu.Fixture) bool { return f.Locale == loc })
		if i < 0 {
			return nil, fmt.Errorf("no NLU fixture for %s", loc)
		}

		out = append(out, all[i])
	}

	return out, nil
}

func newGenOpenCommand() *cobra.Command {
	var (
		sf         smapiFlags
		locales    []string
		invocation string
	)

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Say each locale's launch phrases to Amazon's simulator, against a deployed skill",
		Long: "Says each locale's launch phrases to the simulation API in turn, until one reaches the skill, and reports the locales the simulator refuses and those where no phrase got through.\n" +
			"The skill must be deployed and enabled for testing, with the same invocation choice as --invocation. The simulator takes text, so this can't show how Alexa hears the name.\n" +
			"Credentials come from " + envClientID + ", " + envClientSecret + " and " + envRefreshToken + ".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := skillmodel.Load(i18n.FS)
			if err != nil {
				return err
			}

			if len(locales) == 0 {
				locales = src.LocaleNames()
			}

			for _, loc := range locales {
				if _, ok := src.Locales[loc]; !ok {
					return fmt.Errorf("unknown locale %s (available: %s)", loc, strings.Join(src.LocaleNames(), ", "))
				}
			}

			c, skill, err := sf.client()
			if err != nil {
				return err
			}

			log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelDebug}))

			checks, err := deploy.New(c, src).CheckOpen(cmd.Context(), skill, locales, invocation, log)
			if err != nil {
				return err
			}

			return writeOpenChecks(cmd.OutOrStdout(), checks)
		},
	}

	sf.register(cmd)
	cmd.Flags().StringSliceVarP(&locales, "locale", "l", nil, "locales to try (default all)")
	cmd.Flags().StringVar(&invocation, "invocation", "", "the invocation choice the skill was deployed with: empty for encore, kodi, or a custom name")

	return cmd
}

func writeOpenChecks(w io.Writer, checks []deploy.OpenCheck) error {
	var reached, refused, missed []string

	for _, c := range checks {
		state := "NOT REACHED"

		switch {
		case c.Reached():
			state = "REACHED"
			reached = append(reached, c.Locale)
		case c.Refused():
			state = "REFUSED"
			refused = append(refused, c.Locale)
		default:
			missed = append(missed, c.Locale)
		}

		if _, err := fmt.Fprintf(w, "%-11s %s\n", state, c.Locale); err != nil {
			return err
		}

		for _, t := range c.Tries {
			s := t.Simulation

			var detail string

			switch {
			case s.Invoked:
				detail = "reached the skill"
			case s.Error != "":
				detail = s.Error
			default:
				detail = "Alexa said " + quote(strings.Join(s.Captions, " "))
			}

			if _, err := fmt.Fprintf(w, "  %s: %s\n", quote(t.Phrase), detail); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintf(w, "reached: %s\nrefused by the simulator: %s\nno phrase got through: %s\n", list(reached), list(refused), list(missed))

	return err
}

func list(locales []string) string {
	if len(locales) == 0 {
		return "none"
	}

	return strings.Join(locales, ", ")
}

func quote(s string) string {
	return "“" + s + "”"
}
