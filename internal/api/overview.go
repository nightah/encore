package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/store"
)

func (a *API) registerOverview(api huma.API) {
	ec := &endpointCache{}

	huma.Register(api, protected(huma.Operation{OperationID: "getOverview", Method: http.MethodGet, Path: Prefix + "/overview", Tags: []string{"overview"}, Summary: "Dashboard"}),
		func(ctx context.Context, _ *struct{}) (*body[Overview], error) {
			o, err := a.overview(ctx, ec)

			return &body[Overview]{Body: o}, a.problemFor(ctx, err)
		})
}

func (a *API) overview(ctx context.Context, ec *endpointCache) (Overview, error) {
	var (
		o   Overview
		err error
	)

	if o.Skill, err = a.skill.overview(ctx); err != nil {
		return o, err
	}

	ss, err := a.skill.settings(ctx)
	if err != nil {
		return o, err
	}

	o.Endpoint = a.checkEndpoint(ctx, ec, a.skill.endpointURL(ctx, ss))

	libs, err := a.libraries.List(ctx)
	if err != nil {
		return o, err
	}

	o.Libraries.Count = len(libs)

	for _, l := range libs {
		if len(l.Members) > 1 {
			o.Libraries.SharedGroups++
		}

		if l.LastSyncAt != nil && (o.Libraries.SyncedAt == nil || l.LastSyncAt.After(*o.Libraries.SyncedAt)) {
			o.Libraries.SyncedAt = l.LastSyncAt
		}
	}

	today, err := a.requestsToday(ctx)
	if err != nil {
		return o, err
	}

	o.RequestsToday = summariseToday(today)

	if o.Instances, err = a.instances(ctx); err != nil {
		return o, err
	}

	page, err := a.listRequests(ctx, recentRequests, 0, 0, "", "", true)
	if err != nil {
		return o, err
	}

	o.RecentRequests = page.Items

	if o.Attention, err = a.attention(ctx); err != nil {
		return o, err
	}

	return o, nil
}

// summariseToday counts what Echos asked today; the simulator's requests during deploy smoke tests don't count.
func summariseToday(entries []store.RequestLogEntry) RequestsToday {
	entries = slices.DeleteFunc(slices.Clone(entries), func(e store.RequestLogEntry) bool { return e.Simulated })
	out := RequestsToday{Total: len(entries)}
	durations := make([]int64, 0, len(entries))

	for _, e := range entries {
		if e.Outcome == "error" {
			out.Failed++
		}

		durations = append(durations, e.Duration.Milliseconds())
	}

	if len(durations) > 0 {
		slices.Sort(durations)
		out.MedianMS = durations[len(durations)/2]
	}

	return out
}

// checkEndpoint asks the public endpoint URL for a response. A GET is answered 405 by Encore's Alexa route, which proves the URL reaches this server through the proxy.
func (a *API) checkEndpoint(ctx context.Context, ec *endpointCache, url string) EndpointCheck {
	ec.mu.Lock()
	cached := ec.check
	fresh := ec.url == url && cached.CheckedAt != nil && a.now().Sub(*cached.CheckedAt) < endpointCheckEvery
	ec.mu.Unlock()

	if fresh || !strings.HasPrefix(url, "https://") {
		if !fresh {
			return EndpointCheck{URL: url}
		}

		return cached
	}

	check := EndpointCheck{URL: url}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)

	defer cancel()

	start := time.Now()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, http.NoBody)
	if err == nil {
		resp, derr := a.opts.HTTPClient.Do(req)
		if derr == nil {
			_ = resp.Body.Close()
			check.Reachable = resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode < 500
		}
	}

	check.LatencyMS = time.Since(start).Milliseconds()
	check.CheckedAt = timePtr(a.now())

	ec.mu.Lock()
	ec.url, ec.check = url, check
	ec.mu.Unlock()

	return check
}

// attention is ordered most important first.
func (a *API) attention(ctx context.Context) ([]Attention, error) {
	out := []Attention{}

	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return nil, err
	}

	if len(recs) == 0 {
		out = append(out, Attention{Kind: "no_instances", Title: "No Kodi instance yet", Detail: "Add a Kodi instance so Alexa has something to control.", Link: "/kodi"})
	}

	info, err := a.skill.info(ctx)
	if err != nil {
		return nil, err
	}

	switch {
	case info.Amazon.NeedsReauthorise:
		out = append(out, Attention{Kind: "amazon_reauthorise", Title: "Reconnect to Amazon", Detail: "Amazon no longer accepts Encore's authorisation.", Link: "/alexa-skill"})
	case !info.Amazon.Connected:
		out = append(out, Attention{Kind: "amazon_not_connected", Title: "Connect to Amazon", Detail: "Encore needs Login with Amazon to deploy the skill.", Link: "/alexa-skill"})
	}

	if !info.Privacy.answered() {
		out = append(out, Attention{Kind: "privacy_unanswered", Title: "Answer the privacy questions", Detail: "Amazon requires your own answers before the skill can be deployed.", Link: "/alexa-skill"})
	}

	switch info.State {
	case "failed":
		detail := ""
		if l := info.LastDeploy.V; l != nil {
			detail = l.Error
		}

		out = append(out, Attention{Kind: "deploy_failed", Title: "The last deploy failed", Detail: detail, Link: "/alexa-skill"})
	case "not_deployed":
		out = append(out, Attention{Kind: "skill_not_deployed", Title: "Deploy the skill", Detail: "The Alexa skill has not been deployed yet.", Link: "/alexa-skill"})
	case "live":
		if info.NeedsDeploy && info.UpdateReason == "library" {
			out = append(out, Attention{Kind: "needs_deploy", Title: "New library titles for the skill", Detail: "Encore already finds them. Redeploy to add them to the skill model, which helps Alexa recognise unusual names.", Link: "/alexa-skill"})
		} else if info.NeedsDeploy {
			out = append(out, Attention{Kind: "needs_deploy", Title: "Skill update pending", Detail: "Settings changed since the last deploy.", Link: "/alexa-skill"})
		}
	}

	statuses := a.status.getAll(ctx, a.app.InstanceIDs())

	for _, r := range recs {
		st, ok := statuses[r.ID]

		switch {
		case !ok:
		case st.Unauthorized:
			out = append(out, Attention{Kind: "kodi_unauthorized", Title: r.Name + " rejected Encore's credentials", Detail: "Check the Kodi username and password.", Link: "/kodi"})
		case !st.Reachable && !st.CanWake:
			out = append(out, Attention{Kind: "instance_offline", Title: r.Name + " is offline", Detail: "Encore cannot reach this Kodi instance.", Link: "/kodi"})
		}
	}

	devs, err := a.app.DB().EchoDevices().List(ctx)
	if err != nil {
		return nil, err
	}

	unlinked := 0

	for _, d := range devs {
		if d.KodiInstanceID == 0 {
			unlinked++
		}
	}

	if unlinked > 0 {
		title := "1 Echo device is not linked"
		if unlinked > 1 {
			title = fmt.Sprintf("%d Echo devices are not linked", unlinked)
		}

		out = append(out, Attention{Kind: "unlinked_device", Title: title, Detail: "Unlinked devices control the default Kodi instance.", Link: "/echo-devices"})
	}

	return out, nil
}
