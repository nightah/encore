package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/nlu"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

func (f *smapiFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.skill, "skill", "", "the deployed skill's ID (env "+envSkillID+")")
	cmd.Flags().StringVar(&f.smapiURL, "smapi-url", smapi.BaseURL, "SMAPI's base URL")
	cmd.Flags().StringVar(&f.tokenURL, "token-url", "", "Login with Amazon's token URL")
	cmd.Flags().DurationVar(&f.poll, "poll-interval", 0, "how often to ask SMAPI whether a build is done (default SMAPI's own)")
	_ = cmd.Flags().MarkHidden("smapi-url")
	_ = cmd.Flags().MarkHidden("token-url")
	_ = cmd.Flags().MarkHidden("poll-interval")
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

	c, err := f.clientWith(id, secret, refresh)

	return c, skill, err
}

// clientWith authorises with a security profile's client ID and secret and a refresh token it issued.
func (f *smapiFlags) clientWith(id, secret, refresh string) (*smapi.Client, error) {
	cfg := lwa.Config{Credentials: lwa.Credentials{ClientID: id, ClientSecret: secret}}
	if f.tokenURL != "" {
		cfg.Endpoint = &oauth2.Endpoint{TokenURL: f.tokenURL}
	}

	lc, err := lwa.New(cfg)
	if err != nil {
		return nil, err
	}

	store := &lwa.MemoryStore{}
	// MemoryStore.Save doesn't fail.
	_ = store.Save(context.Background(), &oauth2.Token{RefreshToken: refresh})

	return smapi.New(lc.TokenSource(store), smapi.Options{BaseURL: f.smapiURL, Poll: smapi.PollConfig{Interval: f.poll, MaxInterval: f.poll}})
}

func newGenNLUCommand() *cobra.Command {
	var (
		sf      smapiFlags
		locales []string
		dir     string
		rate    float64
		verbose bool
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

			speech, err := alexa.NewSpeech(src, slog.New(slog.DiscardHandler))
			if err != nil {
				return err
			}

			r := &nlu.Runner{Client: c, SkillID: skill, Rate: rate, Reread: rereader(speech)}

			all, err := r.Run(cmd.Context(), fixtures...)
			if err != nil {
				return err
			}

			if err := nlu.Write(cmd.OutOrStdout(), all, verbose); err != nil {
				return err
			}

			// Throttling the client rode out by retrying costs time without failing a row; it shows how close the run came to Amazon's limit.
			rc := c.Retries()
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "smapi retried %d throttled and %d failed requests, and gave up on %d\n", rc.Throttled, rc.Failed, rc.GaveUp); err != nil {
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
	cmd.Flags().Float64Var(&rate, "rate", 2, "profileNlu requests a second; Amazon throttles a developer account past about 2.3, shared by every skill and locale")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "list the rows that pass too")

	return cmd
}

// rereader reads what NLU heard as Encore's handler does before acting on it: a next-episode request for "the last show" continues watching, a music request whose title names no title is rerouted, and a PlayMedia query is read as it stands, and with each room phrase it may end or start with taken off and kept as the room, as the film, TV and music intents it may be.
func rereader(speech *alexa.Speech) func(locale string, heard nlu.Reading) []nlu.Reading {
	return func(locale string, heard nlu.Reading) []nlu.Reading {
		in := alexa.Intent{Name: heard.Intent, Slots: map[string]alexa.Slot{}}
		for name, value := range heard.Slots {
			in.Slots[name] = alexa.Slot{Name: name, Value: value}
		}

		in = speech.Reroute(locale, speech.LastShow(locale, in))
		out := []nlu.Reading{readingOf(in, "")}

		if in.Name != "PlayMedia" {
			return out
		}

		read := func(title, room string) {
			query := alexa.Intent{Name: "PlayMedia", Slots: map[string]alexa.Slot{"Query": {Name: "Query", Value: title}}}

			for _, in := range speech.Reinterpret(locale, query) {
				out = append(out, readingOf(in, room))
			}
		}

		read(in.Slots["Query"].Value, "")

		for _, split := range speech.RoomSplits(locale, in.Slots["Query"].Value) {
			read(split.Title, split.Room)
		}

		return out
	}
}

// readingOf is in's intent and slot words, with room as its Room when there is one.
func readingOf(in alexa.Intent, room string) nlu.Reading {
	reading := nlu.Reading{Intent: in.Name, Slots: map[string]string{}}
	for name, slot := range in.Slots {
		reading.Slots[name] = slot.Value
	}

	if room != "" {
		reading.Slots["Room"] = room
	}

	return reading
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
			"The skill must be deployed with the same invocation choice as --invocation. The simulator needs it enabled for testing, so a skill that isn't is enabled while it's checked and disabled again after. The simulator takes text, so this can't show how Alexa hears the name.\n" +
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

			restore, err := enableForCheck(cmd.Context(), c, skill, log)
			if err != nil {
				return err
			}

			checks, err := deploy.New(c, src).CheckOpen(cmd.Context(), skill, locales, invocation, log)
			if err := errors.Join(err, restore()); err != nil {
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

// enableForCheck enables skill for testing if it isn't already, as the simulator needs, and returns the func that disables it again, so a skill kept for the checks only answers on your Echos while it's checked. A skill that was enabled is left enabled.
func enableForCheck(ctx context.Context, c *smapi.Client, skill string, log *slog.Logger) (func() error, error) {
	on, err := c.DevelopmentEnabled(ctx, skill)
	if err != nil || on {
		return func() error { return nil }, err
	}

	if err := c.EnableDevelopment(ctx, skill); err != nil {
		return nil, err
	}

	log.Info("enabled the skill for testing while it's checked", "skill", skill)

	return func() error {
		// Disabled even when the check was stopped, so the skill doesn't stay enabled.
		if err := c.DisableDevelopment(context.WithoutCancel(ctx), skill); err != nil {
			return err
		}

		log.Info("disabled the skill for testing again", "skill", skill)

		return nil
	}, nil
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

func newGenDeployCommand() *cobra.Command {
	var (
		sf         smapiFlags
		locales    []string
		invocation string
		vendor     string
		dir        string
		library    string
		retryWait  time.Duration
		retries    int
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy these sources' interaction model to a skill kept for gen nlu and gen open",
		Long: "Builds the skill package from these sources, deploys it to the skill --skill names, waits for every locale's model to build and leaves the skill disabled for testing, so gen nlu and gen open check this model rather than whatever was deployed last, and your Echos never reach it. gen nlu needs no enablement, and gen open enables the skill only while it checks. In place of a library, each locale gets the titles its NLU fixtures ask for, as Encore adds a library's titles to the built-in title types, and with --library-skill the titles a skill Encore deploys already has, such as your own, read from its model.\n" +
			"It only deploys to a skill named " + quote(ciSkillName) + " in every locale it has a name in, so it can't replace the skill Encore deploys for you: create an empty custom skill with that name in the Alexa developer console first. Pick the invocation your own skill doesn't use all the same, as it answers on your Echos while gen open checks it. The endpoint is a placeholder that neither check reaches.\n" +
			"Credentials come from " + envClientID + ", " + envClientSecret + " and " + envRefreshToken + "; the vendor from --vendor or " + envVendorID + ", else the account's only one.",
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

			choice, err := invocationChoice(invocation)
			if err != nil {
				return err
			}

			fixtures, err := nlu.Load(os.DirFS(dir))
			if err != nil {
				return err
			}

			c, skill, err := sf.client()
			if err != nil {
				return err
			}

			vendorID, err := ciVendor(cmd.Context(), c, cmp.Or(vendor, os.Getenv(envVendorID)))
			if err != nil {
				return err
			}

			if err := checkCISkill(cmd.Context(), c, vendorID, skill); err != nil {
				return err
			}

			values := standInLibrary(src, fixtures)

			if from := cmp.Or(library, os.Getenv(envLibrarySkill)); from != "" {
				if values, err = withLibrary(cmd.Context(), c, from, locales, values, cmd.ErrOrStderr()); err != nil {
					return err
				}
			}

			no, yes := false, true
			stderr := cmd.ErrOrStderr()

			res, err := deploy.New(c, src).Deploy(cmd.Context(), deploy.Request{
				State: deploy.State{VendorID: vendorID, SkillID: skill},
				Package: skillpkg.Options{
					Locales:          locales,
					LocaleSlotValues: values,
					Invocation:       choice,
					SkillName:        ciSkillName,
					Endpoint:         skillpkg.Endpoint{URI: ciEndpoint},
					Privacy:          skillpkg.Privacy{AllowsPurchases: &no, UsesPersonalInfo: &no, IsChildDirected: &no, IsExportCompliant: &yes, ContainsAds: &no},
				},
				Force:           true,
				Disabled:        true,
				SilentRetryWait: retryWait,
				SilentRetries:   retries,
				Progress: func(ev deploy.Event) {
					if ev.Message != "" {
						fmt.Fprintf(stderr, "%s %s: %s\n", ev.Step, ev.State, ev.Message)
					}
				},
				Log: slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			})
			if res != nil {
				if werr := writeDeploy(cmd.OutOrStdout(), res); werr != nil && err == nil {
					err = werr
				}
			}

			// A failed import is shown as Amazon answered it, the evidence for a failure it gives no reasons for.
			if de, ok := errors.AsType[*deploy.Error](err); ok && len(de.ImportStatus) > 0 {
				fmt.Fprintf(stderr, "import %s status: %s\n", de.ImportID, de.ImportStatus)
			}

			return err
		},
	}

	sf.register(cmd)
	cmd.Flags().StringSliceVarP(&locales, "locale", "l", nil, "locales to deploy (default all)")
	cmd.Flags().StringVar(&invocation, "invocation", "", "the invocation to deploy with, encore or kodi: the one your own skill doesn't use")
	cmd.Flags().StringVar(&vendor, "vendor", "", "the developer account's vendor ID (env "+envVendorID+")")
	cmd.Flags().StringVar(&dir, "fixtures", defaultNLUFixtures, "directory of <locale>.yaml fixtures whose titles stand in for a library")
	cmd.Flags().StringVar(&library, "library-skill", "", "a skill Encore deploys, whose library titles to add, e.g. your own (env "+envLibrarySkill+"); it is only read")
	cmd.Flags().DurationVar(&retryWait, "retry-wait", 0, "how long to wait before importing again after Amazon fails an import without saying why, and each further time that much longer (default five minutes)")
	cmd.Flags().IntVar(&retries, "silent-retries", 1, "how many times to import again after Amazon fails imports without saying why")
	_ = cmd.Flags().MarkHidden("retry-wait")
	_ = cmd.MarkFlagRequired("invocation")

	return cmd
}

// standInLibrary returns, for each fixture's locale, the values its rows give slots of the standInTypes, as a library with those titles would supply them.
func standInLibrary(src *skillmodel.Sources, fixtures []nlu.Fixture) map[string]map[string][]skillmodel.SlotValue {
	types := map[string]map[string]string{}

	for _, in := range src.Model.Intents {
		types[in.Name] = map[string]string{}
		for _, s := range in.Slots {
			types[in.Name][s.Name] = s.Type
		}
	}

	out := map[string]map[string][]skillmodel.SlotValue{}

	for _, f := range fixtures {
		values := map[string][]skillmodel.SlotValue{}

		for _, c := range f.Cases {
			for slot, value := range c.Slots {
				typ := types[c.Intent][slot]
				if value == "" || !slices.Contains(standInTypes, typ) || slices.ContainsFunc(values[typ], func(v skillmodel.SlotValue) bool { return v.Value == value }) {
					continue
				}

				values[typ] = append(values[typ], skillmodel.SlotValue{Value: value})
			}
		}

		if len(values) > 0 {
			for _, vs := range values {
				slices.SortFunc(vs, func(a, b skillmodel.SlotValue) int { return strings.Compare(a.Value, b.Value) })
			}

			out[f.Locale] = values
		}
	}

	return out
}

// withLibrary adds to values, for each of locales, the standInTypes values in the development model of the skill from, which it only reads. A locale from has no model for keeps values' own; a title already there by value or synonym, ignoring case, isn't added twice.
func withLibrary(ctx context.Context, c *smapi.Client, from string, locales []string, values map[string]map[string][]skillmodel.SlotValue, log io.Writer) (map[string]map[string][]skillmodel.SlotValue, error) {
	out := make(map[string]map[string][]skillmodel.SlotValue, len(locales))

	for _, loc := range locales {
		merged := map[string][]skillmodel.SlotValue{}

		types, err := c.SlotTypes(ctx, from, loc)

		switch {
		case smapi.StatusCode(err) == http.StatusNotFound:
			fmt.Fprintf(log, "library: skill %s has no %s model; using the fixtures' titles\n", from, loc)
		case err != nil:
			return nil, err
		}

		n := 0

		for _, typ := range types {
			if !slices.Contains(standInTypes, typ.Name) {
				continue
			}

			for _, v := range typ.Values {
				merged[typ.Name] = append(merged[typ.Name], skillmodel.SlotValue{ID: v.ID, Value: v.Name.Value, Synonyms: v.Name.Synonyms})
			}

			n += len(typ.Values)
		}

		if len(types) > 0 {
			fmt.Fprintf(log, "library: %d titles in %s from skill %s\n", n, loc, from)
		}

		for typ, vs := range values[loc] {
			for _, v := range vs {
				if !slices.ContainsFunc(merged[typ], func(have skillmodel.SlotValue) bool {
					return strings.EqualFold(have.Value, v.Value) || slices.ContainsFunc(have.Synonyms, func(s string) bool { return strings.EqualFold(s, v.Value) })
				}) {
					merged[typ] = append(merged[typ], v)
				}
			}
		}

		if len(merged) > 0 {
			out[loc] = merged
		}
	}

	return out, nil
}

// invocationChoice turns gen deploy's --invocation into a deploy's invocation choice. Only the two built-in choices are offered, as each locale spells them in a form Amazon takes, which a custom name in Latin letters isn't for ja-JP, hi-IN or ar-SA.
func invocationChoice(name string) (string, error) {
	switch name {
	case "encore":
		return "", nil
	case skillmodel.InvocationKodi:
		return skillmodel.InvocationKodi, nil
	}

	return "", fmt.Errorf("--invocation is encore or kodi, not %q", name)
}

// ciVendor returns vendor, or the account's only vendor when vendor is empty.
func ciVendor(ctx context.Context, c *smapi.Client, vendor string) (string, error) {
	if vendor != "" {
		return vendor, nil
	}

	vendors, err := c.Vendors(ctx)
	if err != nil {
		return "", err
	}

	if len(vendors) != 1 {
		return "", fmt.Errorf("the account has %d developer accounts; name one with --vendor or %s", len(vendors), envVendorID)
	}

	return vendors[0].ID, nil
}

// checkCISkill refuses a skill that isn't the vendor's, or is named anything but ciSkillName in any locale.
func checkCISkill(ctx context.Context, c *smapi.Client, vendorID, skillID string) error {
	skills, err := c.ListSkills(ctx, vendorID)
	if err != nil {
		return err
	}

	for _, s := range skills {
		if s.ID != skillID || s.Stage != "" && s.Stage != smapi.StageDevelopment {
			continue
		}

		if len(s.NameByLocale) == 0 {
			return fmt.Errorf("skill %s has no name; name it %s in the developer console", skillID, quote(ciSkillName))
		}

		for _, loc := range slices.Sorted(maps.Keys(s.NameByLocale)) {
			if name := s.NameByLocale[loc]; name != ciSkillName {
				return fmt.Errorf("skill %s is named %s in %s, not %s; gen deploy only replaces a skill kept for these checks", skillID, quote(name), loc, quote(ciSkillName))
			}
		}

		return nil
	}

	return fmt.Errorf("vendor %s has no skill %s in development", vendorID, skillID)
}

// writeDeploy lists each locale's build status and its problems, and the package's warnings.
func writeDeploy(w io.Writer, res *deploy.Result) error {
	for _, loc := range slices.Sorted(maps.Keys(res.Locales)) {
		st := res.Locales[loc]

		if _, err := fmt.Fprintf(w, "%-11s %s\n", st.Status, loc); err != nil {
			return err
		}

		for _, p := range append(append([]smapi.Problem(nil), st.Errors...), st.Warnings...) {
			if _, err := fmt.Fprintf(w, "  %s\n", p.String()); err != nil {
				return err
			}
		}
	}

	for _, warn := range res.Warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", warn); err != nil {
			return err
		}
	}

	return nil
}
