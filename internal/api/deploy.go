package api

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/lwa"
	"github.com/nightah/encore/internal/smapi/skillpkg"
)

func (d *deployRecord) summary() LastDeploy {
	return LastDeploy{
		ID: d.ID, At: d.At, State: d.State, Hash: d.Hash, Error: d.Error,
		Locales: nonNilLocales(d.Locales), Warnings: nonNil(d.Warnings),
	}
}

func nonNilLocales(l []LocaleDeploy) []LocaleDeploy {
	if l == nil {
		return []LocaleDeploy{}
	}

	return l
}

func (d *deployRecord) view() Deploy {
	events := d.Events
	if events == nil {
		events = []DeployEvent{}
	}

	return Deploy{ID: d.ID, State: d.State, Events: events, Error: d.Error}
}

func (a *API) registerDeploy(api huma.API) {
	s := a.skill

	huma.Register(api, protected(huma.Operation{OperationID: "deploySkill", Method: http.MethodPost, Path: Prefix + "/skill/deploy", Tags: []string{"skill"}, Summary: "Deploy the skill to your Amazon developer account", Description: "Runs in the background, one deploy at a time; progress arrives as deploy events and from GET /skill/deploy/{id}. Refused with 422 until every privacy question is answered.", DefaultStatus: http.StatusAccepted}),
		func(ctx context.Context, in *struct {
			Body *struct {
				Force bool `json:"force,omitempty" doc:"Deploy even when nothing changed"`
			}
		},
		) (*body[Accepted], error) {
			force := in.Body != nil && in.Body.Force

			id, err := s.startDeploy(ctx, force)

			return &body[Accepted]{Body: Accepted{ID: id}}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "getDeploy", Method: http.MethodGet, Path: Prefix + "/skill/deploy/{id}", Tags: []string{"skill"}, Summary: "Get a deploy's progress"}),
		func(ctx context.Context, in *struct {
			ID string `path:"id"`
		},
		) (*body[Deploy], error) {
			d := s.deploy(ctx, in.ID)
			if d == nil {
				return nil, huma.Error404NotFound("not found")
			}

			return &body[Deploy]{Body: d.view()}, nil
		})

	huma.Register(api, protected(huma.Operation{
		OperationID: "downloadSkillPackage", Method: http.MethodGet, Path: Prefix + "/skill/package", Tags: []string{"skill"}, Summary: "Download the skill package", Description: "A zip with skill-package/ and ask-resources.json, for a manual upload or `ask deploy`.",
		Responses: map[string]*huma.Response{"200": {Description: "The skill package", Content: map[string]*huma.MediaType{"application/zip": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}}}},
	}),
		func(ctx context.Context, _ *struct{}) (*struct {
			ContentType        string `header:"Content-Type"`
			ContentDisposition string `header:"Content-Disposition"`
			Body               []byte
		}, error,
		) {
			b, err := s.exportPackage(ctx)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			return &struct {
				ContentType        string `header:"Content-Type"`
				ContentDisposition string `header:"Content-Disposition"`
				Body               []byte
			}{ContentType: "application/zip", ContentDisposition: `attachment; filename="encore-skill-package.zip"`, Body: b}, nil
		})
}

// lastDeploy returns a copy of the most recent deploy, from memory or, after a restart, from settings.
func (s *skillService) lastDeploy(ctx context.Context) *deployRecord {
	s.mu.Lock()

	if n := len(s.order); n > 0 {
		d := s.deploys[s.order[n-1]].clone()
		s.mu.Unlock()

		return d
	}

	s.mu.Unlock()

	var d deployRecord
	if err := s.a.app.DB().Settings().Get(ctx, lastDeployKey, &d); err != nil {
		return nil
	}

	return &d
}

// deploy returns a copy, or nil.
func (s *skillService) deploy(ctx context.Context, id string) *deployRecord {
	s.mu.Lock()
	d, ok := s.deploys[id]

	if ok {
		d = d.clone()
	}
	s.mu.Unlock()

	if ok {
		return d
	}

	if last := s.lastDeploy(ctx); last != nil && last.ID == id {
		return last
	}

	return nil
}

// clone copies a record that a running deploy may still be appending to. Callers hold s.mu.
func (d *deployRecord) clone() *deployRecord {
	c := *d
	c.Events = slices.Clone(d.Events)
	c.Locales = slices.Clone(d.Locales)
	c.Warnings = slices.Clone(d.Warnings)

	return &c
}

func (s *skillService) startDeploy(ctx context.Context, force bool) (string, error) {
	opts, ss, err := s.packageOptions(ctx, false)
	if err != nil {
		return "", err
	}

	if !ss.Privacy.answered() {
		return "", huma.Error422UnprocessableEntity("answer the privacy and compliance questions before deploying",
			&huma.ErrorDetail{Location: "privacy", Message: "unanswered: " + strings.Join(ss.Privacy.unanswered(), ", ")})
	}

	if !strings.HasPrefix(opts.Endpoint.URI, "https://") {
		return "", huma.Error422UnprocessableEntity("set the skill endpoint to the public https:// URL of /api/v1/alexa",
			&huma.ErrorDetail{Location: "endpoint.url", Message: "must be https", Value: opts.Endpoint.URI})
	}

	if _, err := skillpkg.Build(s.a.opts.Sources, opts); err != nil {
		return "", huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "skillpkg: "))
	}

	lc, err := s.lwaClient(ctx, "")
	if err != nil {
		return "", err
	}

	if tok, err := s.tokens().Load(ctx); err != nil || tok == nil {
		return "", huma.Error409Conflict("connect Encore to Amazon first")
	}

	state, err := s.deployState(ctx)
	if err != nil {
		return "", err
	}

	client, err := smapi.New(lc.TokenSource(s.tokens()), s.a.opts.SMAPI)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	if s.running != "" {
		running := s.running
		s.mu.Unlock()

		return "", huma.Error409Conflict("deploy " + running + " is still running")
	}

	id := jobID("d")
	rec := &deployRecord{ID: id, At: s.a.now().UTC(), State: "in_progress"}
	s.running = id
	s.deploys[id] = rec
	s.order = append(s.order, id)

	if len(s.order) > keepDeploys {
		delete(s.deploys, s.order[0])
		s.order = s.order[1:]
	}
	s.mu.Unlock()

	req := deploy.Request{State: state, Package: opts, Force: force, Simulate: true}
	if loc, err := s.a.localeSettings(ctx); err == nil {
		req.PrimaryLocale = loc.DefaultLocale
	}

	s.a.background(func(ctx context.Context) {
		s.runDeploy(ctx, deploy.New(client, s.a.opts.Sources), rec, req)
	})

	return id, nil
}

func (s *skillService) runDeploy(ctx context.Context, d *deploy.Deployer, rec *deployRecord, req deploy.Request) {
	// From the root logger: the API's own already says component=api, and slog would keep both.
	log := s.a.opts.Logger.With("component", "deploy", "deploy", rec.ID)
	req.Log = log

	req.Progress = func(ev deploy.Event) {
		// While the smoke test runs, requests come from Amazon's simulator, not an Echo.
		if ev.Step == deploy.StepSimulate {
			until := s.a.now().Add(simulateGrace)
			if ev.State == deploy.StateRunning {
				until = s.a.now().Add(5 * time.Minute)
			}

			s.simulatingUntil.Store(until.UnixNano())
		}

		de := DeployEvent{Step: string(ev.Step), State: stepState(ev.State), Message: ev.Message, Locales: localeStatuses(ev.Locales), At: s.a.now().UTC()}

		// Build progress repeats every poll; the debug log has it, the info log has each step's start and end.
		if ev.State != deploy.StateRunning || ev.Message != "" {
			log.Info("skill deploy step", "step", ev.Step, "state", ev.State, "message", ev.Message)
		}

		s.mu.Lock()
		rec.Events = append(rec.Events, de)
		s.mu.Unlock()

		s.a.events.publish("deploy", deployEventMessage{DeployID: rec.ID, DeployEvent: de})
	}

	pkg, buildErr := skillpkg.Build(s.a.opts.Sources, req.Package)
	settingsHash := s.settingsHash(req.Package)

	res, err := d.Deploy(ctx, req)

	// Deploy's state is meaningful even after an error, so a retry reuses the skill rather than creating another.
	if res != nil {
		// The settings hash follows Hash: set when this package was fully deployed, cleared when it wasn't.
		res.State.SettingsHash = ""
		if buildErr == nil && res.State.Hash == pkg.Hash() {
			res.State.SettingsHash = settingsHash
		}

		if perr := s.a.app.DB().Settings().Set(context.WithoutCancel(ctx), deployStateKey, res.State); perr != nil {
			s.a.log.Error("saving deploy state", "error", perr)
		}

		if res.State.SkillID != "" {
			s.recordSkillID(context.WithoutCancel(ctx), res.State.SkillID)
		}
	}

	if errors.Is(err, lwa.ErrReauthorise) {
		_ = s.a.app.DB().Settings().Set(context.WithoutCancel(ctx), reauthKey, true)
	}

	s.mu.Lock()

	rec.State = "succeeded"
	if err != nil {
		rec.State, rec.Error = "failed", err.Error()
	}

	if res != nil {
		rec.Hash = res.State.Hash
		rec.Warnings = res.Warnings
		rec.Locales = s.localeResults(res, pkg, buildErr, req.Package)
	}

	snapshot := rec.clone()
	s.running = ""
	s.mu.Unlock()

	if perr := s.a.app.DB().Settings().Set(context.WithoutCancel(ctx), lastDeployKey, snapshot); perr != nil {
		s.a.log.Error("saving deploy record", "error", perr)
	}

	if err != nil {
		s.a.log.Warn("skill deploy failed", "deploy", rec.ID, "error", err)
	} else {
		s.a.log.Info("skill deployed", "deploy", rec.ID, "skill_id", res.State.SkillID, "skipped", res.Skipped)
	}

	if att, aerr := s.a.attention(context.WithoutCancel(ctx)); aerr == nil {
		s.a.status.publishIfChanged("attention", "attention", att)
	}
}

// recordSkillID lets the Alexa endpoint accept the deployed skill's requests when alexa.skill_id is not configured.
func (s *skillService) recordSkillID(ctx context.Context, id string) {
	if err := s.a.app.DB().Settings().Set(ctx, skillIDKey, id); err != nil {
		s.a.log.Error("saving skill ID", "error", err)

		return
	}

	s.skillID.Store(&id)
}

func (s *skillService) localeResults(res *deploy.Result, pkg *skillpkg.Package, buildErr error, opts skillpkg.Options) []LocaleDeploy {
	sizes := map[string]int{}

	if buildErr == nil {
		for _, f := range pkg.Files {
			if loc, ok := strings.CutPrefix(f.Name, skillpkg.ModelDir); ok {
				sizes[strings.TrimSuffix(loc, ".json")] = len(f.Data)
			}
		}
	}

	titles := 0
	for _, vs := range opts.SlotValues {
		titles += len(vs)
	}

	now := s.a.now().UTC()
	locales := slices.Sorted(maps.Keys(res.Locales))

	if res.Skipped || len(locales) == 0 {
		locales = slices.Clone(opts.Locales)
		slices.Sort(locales)
	}

	out := make([]LocaleDeploy, 0, len(locales))

	for _, loc := range locales {
		ld := LocaleDeploy{Locale: loc, Status: "succeeded", Errors: []string{}, Titles: titles, SizeBytes: sizes[loc], BuiltAt: &now}

		if ls, ok := res.Locales[loc]; ok {
			ld.Status = localeStatus(ls.Status)

			for _, p := range ls.Errors {
				ld.Errors = append(ld.Errors, p.String())
			}
		}

		out = append(out, ld)
	}

	return out
}

func stepState(s deploy.StepState) string {
	switch s {
	case deploy.StateRunning:
		return "started"
	case deploy.StateDone:
		return "succeeded"
	case deploy.StateSkipped:
		return "skipped"
	default:
		return "failed"
	}
}

func localeStatus(s smapi.Status) string {
	switch s {
	case smapi.StatusSucceeded:
		return "succeeded"
	case smapi.StatusFailed:
		return "failed"
	default:
		return "in_progress"
	}
}

func localeStatuses(in map[string]deploy.LocaleStatus) map[string]LocaleStatus {
	out := make(map[string]LocaleStatus, len(in))

	for loc, ls := range in {
		st := LocaleStatus{Status: localeStatus(ls.Status), Errors: []string{}}
		for _, p := range ls.Errors {
			st.Errors = append(st.Errors, p.String())
		}

		out[loc] = st
	}

	return out
}

func (s *skillService) overview(ctx context.Context) (OverviewSkill, error) {
	state, err := s.deployState(ctx)
	if err != nil {
		return OverviewSkill{}, err
	}

	locales, err := s.locales(ctx)
	if err != nil {
		return OverviewSkill{}, err
	}

	out := OverviewSkill{State: s.stateOf(state, s.lastDeploy(ctx)), Stage: string(smapi.StageDevelopment), Locales: locales}
	out.NeedsDeploy, out.UpdateReason = s.updatePending(ctx, state)

	return out, nil
}
