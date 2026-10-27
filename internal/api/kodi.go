package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

// jobSeq numbers background jobs (deploys, syncs, ...). It starts from the process start time, so IDs don't repeat across restarts and can't be mistaken for the last deploy saved before one.
var jobSeq = func() *atomic.Int64 {
	var n atomic.Int64
	n.Store(time.Now().Unix() * 1000)

	return &n
}()

func jobID(kind string) string { return kind + "-" + strconv.FormatInt(jobSeq.Add(1), 36) }

func (a *API) registerKodi(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "listKodiInstances", Method: http.MethodGet, Path: Prefix + "/kodi/instances", Tags: []string{"kodi"}, Summary: "List Kodi instances"}),
		func(ctx context.Context, _ *struct{}) (*body[[]Instance], error) {
			out, err := a.instances(ctx)

			return &body[[]Instance]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "createKodiInstance", Method: http.MethodPost, Path: Prefix + "/kodi/instances", Tags: []string{"kodi"}, Summary: "Add a Kodi instance", DefaultStatus: http.StatusCreated}),
		func(ctx context.Context, in *struct{ Body InstanceInput }) (*body[Instance], error) {
			inst, err := a.createInstance(ctx, in.Body)

			return &body[Instance]{Body: inst}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "getKodiInstance", Method: http.MethodGet, Path: Prefix + "/kodi/instances/{id}", Tags: []string{"kodi"}, Summary: "Get a Kodi instance"}),
		func(ctx context.Context, in *idPath) (*body[Instance], error) {
			inst, err := a.instance(ctx, in.ID)

			return &body[Instance]{Body: inst}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateKodiInstance", Method: http.MethodPatch, Path: Prefix + "/kodi/instances/{id}", Tags: []string{"kodi"}, Summary: "Change a Kodi instance"}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body InstanceInput
		},
		) (*body[Instance], error) {
			inst, err := a.updateInstance(ctx, in.ID, in.Body)

			return &body[Instance]{Body: inst}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "deleteKodiInstance", Method: http.MethodDelete, Path: Prefix + "/kodi/instances/{id}", Tags: []string{"kodi"}, Summary: "Remove a Kodi instance", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, a.problemFor(ctx, a.deleteInstance(ctx, in.ID))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "testKodiInstance", Method: http.MethodPost, Path: Prefix + "/kodi/instances/{id}/test", Tags: []string{"kodi"}, Summary: "Test the connection to a Kodi instance"}),
		func(ctx context.Context, in *idPath) (*body[TestResult], error) {
			out := &body[TestResult]{}

			checks, err := a.testInstance(ctx, in.ID)
			out.Body.Checks = checks

			return out, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "syncKodiInstance", Method: http.MethodPost, Path: Prefix + "/kodi/instances/{id}/sync", Tags: []string{"kodi"}, Summary: "Reload a Kodi instance's library", DefaultStatus: http.StatusAccepted}),
		func(ctx context.Context, in *idPath) (*body[Accepted], error) {
			if _, err := a.app.InstanceStatus(ctx, in.ID, false); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			id := jobID("sync")
			a.background(func(ctx context.Context) {
				if err := a.app.SyncInstance(ctx, in.ID); err != nil {
					a.log.Warn("library sync failed", "kodi_instance", in.ID, "error", err)
				}

				a.status.forget(in.ID)
				a.status.poll(ctx)
			})

			return &body[Accepted]{Body: Accepted{ID: id}}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "wakeKodiInstance", Method: http.MethodPost, Path: Prefix + "/kodi/instances/{id}/wake", Tags: []string{"kodi"}, Summary: "Wake a Kodi instance with Wake-on-LAN", DefaultStatus: http.StatusAccepted}),
		func(ctx context.Context, in *idPath) (*body[Accepted], error) {
			st, err := a.app.InstanceStatus(ctx, in.ID, false)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			if !st.CanWake {
				return nil, huma.Error409Conflict("no MAC address is known for this Kodi instance; set one, or let Encore learn it while Kodi is awake")
			}

			id := jobID("wake")
			a.background(func(ctx context.Context) {
				if err := a.app.WakeInstance(ctx, in.ID, wakeTimeout); err != nil {
					a.log.Info("wake did not complete", "kodi_instance", in.ID, "error", err)
				}

				a.status.forget(in.ID)
				a.status.poll(ctx)
			})

			return &body[Accepted]{Body: Accepted{ID: id}}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "setDefaultKodiInstance", Method: http.MethodPost, Path: Prefix + "/kodi/instances/{id}/default", Tags: []string{"kodi"}, Summary: "Make a Kodi instance the default for unlinked Echo devices", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, a.problemFor(ctx, a.app.DB().KodiInstances().SetDefault(ctx, in.ID))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "listDiscoveredKodi", Method: http.MethodGet, Path: Prefix + "/kodi/discovered", Tags: []string{"kodi"}, Summary: "Find Kodi instances on the network with mDNS"}),
		func(ctx context.Context, _ *struct{}) (*body[[]DiscoveredInstance], error) {
			out, err := a.discover(ctx)

			return &body[[]DiscoveredInstance]{Body: out}, a.problemFor(ctx, err)
		})
}

func (a *API) instances(ctx context.Context) ([]Instance, error) {
	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, r.ID)
	}

	statuses := a.status.getAll(ctx, ids)
	out := make([]Instance, 0, len(recs))

	for _, r := range recs {
		st, ok := statuses[r.ID]
		out = append(out, a.toInstance(ctx, r, st, ok))
	}

	return out, nil
}

func (a *API) instance(ctx context.Context, id int64) (Instance, error) {
	rec, err := a.app.DB().KodiInstances().Get(ctx, id)
	if err != nil {
		return Instance{}, err
	}

	st, err := a.status.get(ctx, id)

	return a.toInstance(ctx, rec, st, err == nil), nil
}

func (a *API) managedInstance(name string) bool {
	for _, k := range a.app.Config().Kodi {
		if strings.EqualFold(k.Name, name) {
			return true
		}
	}

	return false
}

func (a *API) toInstance(ctx context.Context, rec store.KodiInstance, st app.InstanceStatus, loaded bool) Instance {
	inst := Instance{
		ID:                 rec.ID,
		Name:               rec.Name,
		URL:                httpURL(rec),
		WebSocketURL:       wsURL(rec),
		Username:           rec.Username,
		HasPassword:        rec.PasswordSecretRef != "",
		MAC:                rec.MAC,
		Broadcast:          rec.BroadcastAddr,
		DefaultForUnlinked: rec.IsDefaultForUnlinked,
		Language:           cmp.Or(rec.Language, "en"),
		Timezone:           rec.Timezone,
		Shutdown:           shutdownOf(rec.ShutdownQuits),
		ManagedByConfig:    a.managedInstance(rec.Name),
		Status:             InstanceStatus{State: "offline"},
	}

	if lib, err := a.libraries.LibraryOf(ctx, rec.ID); err == nil {
		inst.Library.GroupID = lib.ID
		inst.Library.Shared = len(lib.Members) > 1
		inst.Library.IsSource = lib.SourceInstanceID == rec.ID
	}

	if !loaded {
		return inst
	}

	inst.Status = InstanceStatus{
		State:       stateOf(st),
		KodiVersion: st.KodiVersion,
		APIVersion:  st.APIVersion,
		LastEventAt: timePtr(st.LastEventAt),
		LastSyncAt:  timePtr(st.LastSyncAt),
		NextSyncAt:  timePtr(st.NextSyncAt),
	}

	inst.Library.Movies, inst.Library.Shows, inst.Library.Episodes, inst.Library.Songs = st.Counts.Movies, st.Counts.Shows, st.Counts.Episodes, st.Counts.Songs

	if n := st.Newest; n != nil {
		title := n.Title
		if n.Kind == media.KindEpisode && n.ShowTitle != "" {
			title = n.ShowTitle + ": " + n.Title
		}

		inst.Library.NewestTitle = Some(NewestTitle{Title: title, Year: n.Year, Findable: a.app.Findable(rec.ID, n.Ref())})
	}

	if np := st.NowPlaying; np != nil {
		inst.NowPlaying = Some(NowPlaying{
			Kind: string(np.Item.Kind), Title: np.Item.Title, Show: np.Item.ShowTitle,
			Season: np.Item.Season, Episode: np.Item.Episode,
			PositionS: int(np.Position.Seconds()), DurationS: int(np.Duration.Seconds()), Paused: np.Paused,
			ArtURL: artURL(rec.ID, np.Item.Art.Cover()),
		})
	}

	return inst
}

func stateOf(st app.InstanceStatus) string {
	switch {
	case st.Connected:
		return "connected"
	case st.Reachable:
		return "http_only"
	case st.CanWake:
		return "asleep"
	default:
		return "offline"
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	t = t.UTC()

	return &t
}

func httpURL(rec store.KodiInstance) string {
	u := url.URL{Scheme: rec.Scheme, Host: net.JoinHostPort(rec.Host, strconv.Itoa(rec.Port)), Path: rec.Path}

	return u.String()
}

func wsURL(rec store.KodiInstance) string {
	if rec.WSPort == 0 {
		return ""
	}

	scheme := "ws"
	if rec.Scheme == "https" {
		scheme = "wss"
	}

	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(rec.Host, strconv.Itoa(rec.WSPort)), Path: rec.Path}

	return u.String()
}

func passwordRef(name string) string { return "kodi/" + name + "/password" }

// applyInstanceInput returns the new password: nil for unchanged, "" to remove.
func applyInstanceInput(rec *store.KodiInstance, in InstanceInput, creating bool) (*string, error) {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fieldError("body.name", "must not be empty", *in.Name)
		}

		rec.Name = name
	}

	if creating && (in.Name == nil || in.URL == nil) {
		return nil, fieldError("body", "name and url are required", nil)
	}

	password := in.Password

	if in.URL != nil || in.WebSocketURL != nil {
		httpRaw := httpURL(*rec)
		if in.URL != nil {
			httpRaw = strings.TrimSpace(*in.URL)
		}

		wsRaw := wsURL(*rec)
		if in.WebSocketURL != nil {
			wsRaw = strings.TrimSpace(*in.WebSocketURL)
		}

		u, err := url.Parse(httpRaw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return nil, fieldError("body.url", "must be an http(s) URL such as http://192.168.1.20:8080/jsonrpc", redactURL(httpRaw))
		}

		if wsRaw != "" {
			w, err := url.Parse(wsRaw)
			if err != nil || (w.Scheme != "ws" && w.Scheme != "wss") || w.Hostname() == "" {
				return nil, fieldError("body.websocket_url", "must be a ws(s) URL such as ws://192.168.1.20:9090/jsonrpc", wsRaw)
			}
		}

		kc, err := kodi.ConfigFromURLs(httpRaw, wsRaw)
		if err != nil {
			return nil, fieldError("body.url", err.Error(), redactURL(httpRaw))
		}

		rec.Scheme, rec.Host, rec.Port, rec.Path = kc.Scheme, kc.Host, kc.Port, kc.Path
		rec.WSPort = 0

		if wsRaw != "" {
			rec.WSPort = kc.WebSocketPort
		}

		// Credentials in the URL are moved to their own fields so they are never shown back.
		if kc.Username != "" && in.Username == nil {
			rec.Username = kc.Username
		}

		if kc.Password != "" && password == nil {
			password = &kc.Password
		}
	}

	if in.Username != nil {
		rec.Username = strings.TrimSpace(*in.Username)
	}

	if in.MAC != nil {
		mac := strings.TrimSpace(*in.MAC)
		if mac != "" {
			hw, err := net.ParseMAC(mac)
			if err != nil {
				return nil, fieldError("body.mac", "is not a MAC address", mac)
			}

			mac = strings.ToUpper(hw.String())
		}

		rec.MAC = mac
	}

	if in.Broadcast != nil {
		b := strings.TrimSpace(*in.Broadcast)
		if b != "" && net.ParseIP(b) == nil {
			return nil, fieldError("body.broadcast", "must be an IP address such as 192.168.1.255", b)
		}

		rec.BroadcastAddr = b
	}

	return password, applyInstanceBehaviour(rec, in)
}

func applyInstanceBehaviour(rec *store.KodiInstance, in InstanceInput) error {
	if in.Language != nil {
		lang := strings.TrimSpace(*in.Language)
		if !slices.Contains(match.Languages(), lang) {
			return fieldError("body.language", "must be one of "+strings.Join(match.Languages(), ", "), lang)
		}

		rec.Language = lang
	}

	if in.Timezone != nil {
		tz := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(tz); tz != "" && (err != nil || strings.EqualFold(tz, "local")) {
			return fieldError("body.timezone", "is not an IANA time zone such as Australia/Melbourne", tz)
		}

		rec.Timezone = tz
	}

	if in.Shutdown != nil {
		switch *in.Shutdown {
		case shutdownQuit:
			rec.ShutdownQuits = new(true)
		case shutdownPowerOff:
			rec.ShutdownQuits = new(false)
		default:
			rec.ShutdownQuits = nil
		}
	}

	return nil
}

func shutdownOf(q *bool) string {
	switch {
	case q == nil:
		return shutdownInherit
	case *q:
		return shutdownQuit
	default:
		return shutdownPowerOff
	}
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}

	return u.Redacted()
}

func (a *API) createInstance(ctx context.Context, in InstanceInput) (Instance, error) {
	rec := store.KodiInstance{Scheme: "http", Path: "/jsonrpc"}

	password, err := applyInstanceInput(&rec, in, true)
	if err != nil {
		return Instance{}, err
	}

	if a.managedInstance(rec.Name) {
		return Instance{}, huma.Error409Conflict(fmt.Sprintf("a Kodi instance named %q is declared in config", rec.Name))
	}

	if err := a.savePassword(ctx, &rec, password); err != nil {
		return Instance{}, err
	}

	// The first instance answers unlinked Echos until told otherwise.
	if existing, err := a.app.DB().KodiInstances().List(ctx); err == nil && len(existing) == 0 {
		rec.IsDefaultForUnlinked = true
	}

	if err := a.app.DB().KodiInstances().Create(ctx, &rec); err != nil {
		return Instance{}, err
	}

	if err := a.reloadRuntime(ctx, rec.ID); err != nil {
		return Instance{}, err
	}

	return a.instance(ctx, rec.ID)
}

func (a *API) updateInstance(ctx context.Context, id int64, in InstanceInput) (Instance, error) {
	repo := a.app.DB().KodiInstances()

	rec, err := repo.Get(ctx, id)
	if err != nil {
		return Instance{}, err
	}

	if a.managedInstance(rec.Name) {
		return Instance{}, managedError("kodi instance " + strconv.Quote(rec.Name))
	}

	oldName := rec.Name

	password, err := applyInstanceInput(&rec, in, false)
	if err != nil {
		return Instance{}, err
	}

	if rec.Name != oldName && a.managedInstance(rec.Name) {
		return Instance{}, huma.Error409Conflict(fmt.Sprintf("a Kodi instance named %q is declared in config", rec.Name))
	}

	if rec.Name != oldName && password == nil && rec.PasswordSecretRef != "" {
		// The secret is keyed by name; carry it over.
		old, err := a.app.Secrets().Get(ctx, rec.PasswordSecretRef)
		if err == nil {
			s := string(old)
			password = &s

			clear(old)
		}
	}

	oldRef := rec.PasswordSecretRef

	if err := a.savePassword(ctx, &rec, password); err != nil {
		return Instance{}, err
	}

	if err := repo.Update(ctx, &rec); err != nil {
		return Instance{}, err
	}

	if oldRef != "" && oldRef != rec.PasswordSecretRef {
		_ = a.app.Secrets().Delete(ctx, oldRef)
	}

	if err := a.reloadRuntime(ctx, id); err != nil {
		return Instance{}, err
	}

	return a.instance(ctx, id)
}

// savePassword keys the secret by rec's name. A nil password leaves it unchanged.
func (a *API) savePassword(ctx context.Context, rec *store.KodiInstance, password *string) error {
	if password == nil {
		return nil
	}

	if *password == "" {
		if rec.PasswordSecretRef != "" {
			if err := a.app.Secrets().Delete(ctx, rec.PasswordSecretRef); err != nil {
				return err
			}
		}

		rec.PasswordSecretRef = ""

		return nil
	}

	ref := passwordRef(rec.Name)
	if err := a.app.Secrets().Put(ctx, ref, []byte(*password)); err != nil {
		return err
	}

	rec.PasswordSecretRef = ref

	return nil
}

func (a *API) deleteInstance(ctx context.Context, id int64) error {
	rec, err := a.app.DB().KodiInstances().Get(ctx, id)
	if err != nil {
		return err
	}

	if a.managedInstance(rec.Name) {
		return managedError("kodi instance " + strconv.Quote(rec.Name))
	}

	if err := a.app.DB().KodiInstances().Delete(ctx, id); err != nil {
		return err
	}

	if rec.PasswordSecretRef != "" {
		if err := a.app.Secrets().Delete(ctx, rec.PasswordSecretRef); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			a.log.Warn("deleting Kodi password", "instance", rec.Name, "backend", "kodi", "error", err)
		}
	}

	if err := a.app.RemoveInstance(ctx, id); err != nil {
		return err
	}

	a.status.forget(id)

	return nil
}

// reloadRuntime replaces an instance's runtime so saved changes take effect without a restart.
func (a *API) reloadRuntime(ctx context.Context, id int64) error {
	if err := a.app.ReloadInstance(ctx, id); err != nil {
		return err
	}

	a.status.forget(id)

	return nil
}

func (a *API) testInstance(ctx context.Context, id int64) ([]Check, error) {
	rec, err := a.app.DB().KodiInstances().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	password := ""

	if rec.PasswordSecretRef != "" {
		b, err := a.app.Secrets().Get(ctx, rec.PasswordSecretRef)
		if err != nil {
			return nil, err
		}

		password = string(b)
		clear(b)
	}

	checks := a.app.TestConnection(ctx, rec, password)
	out := make([]Check, 0, len(checks))

	for _, c := range checks {
		out = append(out, Check{Name: c.Name, OK: c.OK, MS: c.Duration.Milliseconds(), Detail: c.Detail})
	}

	return out, nil
}

func (a *API) discover(ctx context.Context) ([]DiscoveredInstance, error) {
	find := a.opts.Discover
	if find == nil {
		find = kodi.Discoverer{}.Discover
	}

	found, err := find(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering Kodi instances: %w", err)
	}

	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return nil, err
	}

	out := []DiscoveredInstance{}

	for _, f := range found {
		cfg := f.Config()

		configured := false

		for _, r := range recs {
			if strings.EqualFold(r.Host, cfg.Host) && (r.Port == cfg.Port || f.Port == 0) {
				configured = true

				break
			}
		}

		if !configured {
			out = append(out, DiscoveredInstance{Name: f.Name, Host: cfg.Host, HTTPPort: f.Port, WSPort: f.WebSocketPort})
		}
	}

	a.probeVersions(ctx, out)

	return out, nil
}

// probeVersions asks every instance at once; one that can't be asked keeps an empty version.
func (a *API) probeVersions(ctx context.Context, found []DiscoveredInstance) {
	probe := a.opts.ProbeVersion
	if probe == nil {
		probe = kodi.ProbeVersion
	}

	var wg sync.WaitGroup

	for i := range found {
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()

			if v, err := probe(pctx, found[i].Host, found[i].WSPort); err == nil {
				found[i].Version = v
			}
		})
	}

	wg.Wait()
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// artURL proxies the image through Encore; "" without a ref.
func artURL(instanceID int64, ref string) string {
	if ref == "" {
		return ""
	}

	return Prefix + "/kodi/instances/" + strconv.FormatInt(instanceID, 10) + "/art?ref=" + url.QueryEscape(ref)
}
