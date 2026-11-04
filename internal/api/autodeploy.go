package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/smapi/deploy"
	"github.com/nightah/encore/internal/smapi/skillpkg"
	"github.com/nightah/encore/internal/store"
)

// runAutoDeploy looks for skill changes to deploy every alexa.auto_deploy_minutes until ctx is done. A change is deployed once it has stayed the same for a whole check, so a library sync or a run of settings changes is deployed once, after it settles.
func (s *skillService) runAutoDeploy(ctx context.Context) {
	every(ctx, s.autoDeployInterval, s.autoDeploy)
}

// autoDeployInterval is the effective alexa.auto_deploy_minutes.
func (s *skillService) autoDeployInterval(ctx context.Context) time.Duration {
	minutes := float64(config.DefaultAutoDeployMinutes)
	if eff, err := s.a.prefs.Effective(ctx); err == nil {
		minutes = eff.Alexa.AutoDeployMinutes
	}

	return time.Duration(minutes * float64(time.Minute))
}

// every calls check each time interval() has passed since the last call, until ctx is done. interval is read again at least every autoDeployTick, so a change applies within a tick rather than after the wait already running.
func every(ctx context.Context, interval func(context.Context) time.Duration, check func(context.Context)) {
	last := time.Now()

	for {
		wait := min(time.Until(last.Add(interval(ctx))), autoDeployTick)
		if wait <= 0 {
			last = time.Now()

			check(ctx)

			continue
		}

		t := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			t.Stop()

			return
		case <-t.C:
		}
	}
}

// autoDeploy starts a deploy when alexa.auto_deploy is on, automatic deploys aren't paused, the skill has been deployed before and Amazon is connected, and what would be deployed differs from the last deploy and hasn't changed since the previous check. A failed automatic deploy waiting to be tried again is retried once its wait is over, without waiting for the package to settle, since it had before the first attempt. A deploy that can't start pauses automatic deploys, as a failed one does. While automatic deploys are off or paused, a failure waiting to be tried again is forgotten.
func (s *skillService) autoDeploy(ctx context.Context) {
	eff, err := s.a.prefs.Effective(ctx)
	if err != nil || !eff.Alexa.AutoDeploy || s.autoDeployPaused(ctx) != "" {
		// A failure waiting to be tried again is forgotten too, so automatic deploys turned back on wait for a change to settle and count its failures afresh.
		s.autoSeen = ""
		s.setAutoDeployRetry(ctx, nil)

		return
	}

	state, err := s.deployState(ctx)
	if err != nil || state.SkillID == "" {
		return
	}

	// The attention list already asks for these; a deploy can't succeed without them.
	if tok, err := s.tokens().Load(ctx); err != nil || tok == nil {
		return
	}

	if reauth, _ := store.Setting(ctx, s.a.app.DB().Settings(), reauthKey, false); reauth {
		return
	}

	if err := s.recoverEndpoint(ctx, state.SkillID); err != nil {
		s.a.log.Warn("reading the deployed skill's endpoint from Amazon", "error", err)

		return
	}

	retry := s.autoDeployRetry(ctx)
	if retry != nil && s.a.now().Before(retry.NextAt) {
		return
	}

	opts, _, err := s.packageOptions(ctx, false)
	if err != nil {
		return
	}

	hash := ""
	if pkg, err := skillpkg.Build(s.a.opts.Sources, opts); err == nil {
		if !state.NeedsDeploy(pkg.Hash()) {
			s.autoSeen = ""

			return
		}

		hash = pkg.Hash()
	}

	// A package that doesn't build is left to startDeploy to explain, once it has stayed that way for an interval too.
	if hash == "" {
		hash = "unbuildable"
	}

	if hash != s.autoSeen && retry == nil {
		s.autoSeen = hash

		return
	}

	s.autoSeen = ""

	id, err := s.startDeploy(ctx, false, true)

	var se huma.StatusError

	switch {
	case err == nil:
		s.a.log.Info("automatic skill deploy started", "deploy", id)
	case errors.As(err, &se) && se.GetStatus() == http.StatusConflict:
		// A deploy started by hand is already running.
	default:
		s.a.log.Warn("automatic skill deploy could not start", "error", err)
		s.setAutoDeployRetry(ctx, nil)
		s.setAutoDeployPaused(ctx, err.Error())

		if att, aerr := s.a.attention(ctx); aerr == nil {
			s.a.status.publishIfChanged("attention", "attention", att)
		}
	}
}

// recoverEndpoint reads the endpoint from the deployed skill's manifest when nothing local says what it is: no endpoint is configured and none was recorded, as for a skill deployed before Encore recorded it, or after its record was lost. A manifest without one leaves the endpoint unknown, so the deploy that follows says to set it.
func (s *skillService) recoverEndpoint(ctx context.Context, skillID string) error {
	ss, err := s.settings(ctx)
	if err != nil {
		return err
	}

	if s.endpointURL(ctx, ss) != "" {
		return nil
	}

	lc, err := s.lwaClient(ctx, "")
	if err != nil {
		return err
	}

	// New fails only without a token source, and lc always gives one.
	client, _ := smapi.New(lc.TokenSource(s.tokens()), s.a.opts.SMAPI)

	uri, err := client.SkillEndpoint(ctx, skillID)
	if err != nil {
		return err
	}

	if uri != "" {
		s.rememberEndpoint(ctx, uri)
		s.a.log.Info("recovered the skill endpoint from Amazon", "endpoint", uri)
	}

	return nil
}

// autoDeployPaused is why automatic deploys are paused, or "" when they aren't.
func (s *skillService) autoDeployPaused(ctx context.Context) string {
	reason, _ := store.Setting(ctx, s.a.app.DB().Settings(), autoDeployPausedKey, "")

	return reason
}

// setAutoDeployPaused pauses automatic deploys for reason, or resumes them when reason is "".
func (s *skillService) setAutoDeployPaused(ctx context.Context, reason string) {
	if s.autoDeployPaused(ctx) == reason {
		return
	}

	if err := s.a.app.DB().Settings().Set(ctx, autoDeployPausedKey, strings.TrimSpace(reason)); err != nil {
		s.a.log.Error("saving automatic deploy pause", "error", err)
	}
}

// autoDeployFailed handles an automatic deploy that failed with err: one that may pass is tried again after a growing wait, up to autoDeployRetries times; anything else, or one failure too many, pauses automatic deploys.
func (s *skillService) autoDeployFailed(ctx context.Context, err error) {
	failures := 1
	if prev := s.autoDeployRetry(ctx); prev != nil {
		failures = prev.Failures + 1
	}

	if deploy.Transient(err) && failures <= autoDeployRetries {
		next := s.a.now().UTC().Add(autoDeployRetryDelay << (failures - 1))
		s.a.log.Info("automatic skill deploy will be tried again", "attempt", failures, "next_at", next, "error", err)
		s.setAutoDeployRetry(ctx, &autoDeployRetry{Failures: failures, NextAt: next, Error: err.Error()})

		return
	}

	s.setAutoDeployRetry(ctx, nil)
	s.setAutoDeployPaused(ctx, err.Error())
}

// autoDeployRetry is the failed automatic deploy waiting to be tried again, or nil.
func (s *skillService) autoDeployRetry(ctx context.Context) *autoDeployRetry {
	var r autoDeployRetry
	if err := s.a.app.DB().Settings().Get(ctx, autoDeployRetryKey, &r); err != nil {
		return nil
	}

	return &r
}

// setAutoDeployRetry records the failed automatic deploy to try again, or forgets it when r is nil.
func (s *skillService) setAutoDeployRetry(ctx context.Context, r *autoDeployRetry) {
	var err error

	switch {
	case r != nil:
		err = s.a.app.DB().Settings().Set(ctx, autoDeployRetryKey, r)
	case s.autoDeployRetry(ctx) != nil:
		err = s.a.app.DB().Settings().Delete(ctx, autoDeployRetryKey)
	}

	if err != nil {
		s.a.log.Error("saving automatic deploy retry", "error", err)
	}
}
