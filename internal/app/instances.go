package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/store"
)

func passwordRef(name string) string { return "kodi/" + name + "/password" }

// applyConfiguredInstances matches config entries to rows by name.
func (a *App) applyConfiguredInstances(ctx context.Context) error {
	if len(a.cfg.Kodi) == 0 {
		return nil
	}

	existing, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return err
	}

	byName := map[string]store.KodiInstance{}
	for _, k := range existing {
		byName[k.Name] = k
	}

	for _, c := range a.cfg.Kodi {
		kc, err := kodi.ConfigFromURLs(c.URL, c.WebSocketURL)
		if err != nil {
			return fmt.Errorf("kodi instance %q: %w", c.Name, err)
		}

		rec := byName[c.Name]
		rec.Name, rec.Scheme, rec.Host, rec.Port, rec.Path = c.Name, kc.Scheme, kc.Host, kc.Port, kc.Path
		rec.WSPort, rec.MAC, rec.BroadcastAddr = kc.WebSocketPort, c.MAC, c.Broadcast
		rec.Language, rec.Timezone, rec.ShutdownQuits = c.Language, c.Timezone, c.ShutdownQuits

		rec.Username = firstNonEmpty(c.Username, kc.Username)
		if password := firstNonEmpty(c.Password, kc.Password); password != "" {
			if err := a.secrets.Put(ctx, passwordRef(c.Name), []byte(password)); err != nil {
				return fmt.Errorf("kodi instance %q: storing password: %w", c.Name, err)
			}

			rec.PasswordSecretRef = passwordRef(c.Name)
		}

		if rec.ID == 0 {
			err = a.db.KodiInstances().Create(ctx, &rec)
		} else {
			err = a.db.KodiInstances().Update(ctx, &rec)
		}

		if err != nil {
			return fmt.Errorf("kodi instance %q: %w", c.Name, err)
		}

		if c.Default {
			if err := a.db.KodiInstances().SetDefault(ctx, rec.ID); err != nil {
				return fmt.Errorf("kodi instance %q: %w", c.Name, err)
			}
		}
	}

	return nil
}

func (a *App) loadInstances(ctx context.Context) error {
	records, err := a.db.KodiInstances().List(ctx)
	if err != nil {
		return err
	}

	for _, rec := range records {
		inst, err := a.newInstance(ctx, rec)
		if err != nil {
			return fmt.Errorf("kodi instance %q: %w", rec.Name, err)
		}

		a.instances[rec.ID] = inst
	}

	if len(records) == 0 {
		a.log.Warn("no Kodi instances configured; add one in the web interface or under kodi: in the config file")
	}

	return nil
}

func (a *App) newInstance(ctx context.Context, rec store.KodiInstance) (*Instance, error) {
	// instance names it whatever kind of media server it is; backend says the kind.
	log := a.log.With("instance", rec.Name, "backend", "kodi")

	kc := kodi.Config{
		Scheme:           rec.Scheme,
		Host:             rec.Host,
		Port:             rec.Port,
		WebSocketPort:    rec.WSPort,
		Path:             rec.Path,
		Username:         rec.Username,
		DisableWebSocket: rec.WSPort == 0,
	}

	if rec.PasswordSecretRef != "" {
		password, err := a.secrets.Get(ctx, rec.PasswordSecretRef)
		if err != nil {
			return nil, fmt.Errorf("reading password: %w", err)
		}

		kc.Password = string(password)
	}

	client := kodi.NewClient(kc, log.With("component", "kodi"))
	backend := kodi.NewBackend(client, kodi.BackendOptions{Name: rec.Name, Location: timezone(rec.Timezone), Logger: log})
	waker := &kodi.Waker{Client: client, Broadcast: rec.BroadcastAddr}

	if rec.MAC != "" {
		mac, err := net.ParseMAC(rec.MAC)
		if err != nil {
			return nil, fmt.Errorf("parsing MAC %q: %w", rec.MAC, err)
		}

		waker.SetMAC(mac)
	}

	return &Instance{Record: rec, Client: client, Backend: backend, Waker: waker, id: rec.ID, name: rec.Name, log: log, db: a.db}, nil
}

// timezone returns nil, which means learn the offset from Kodi's clock, for an empty or unknown zone. Config and API validation have already checked it.
func timezone(name string) *time.Location {
	if name == "" {
		return nil
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}

	return loc
}

func (i *Instance) language() string {
	if i.Record.Language != "" {
		return i.Record.Language
	}

	return "en"
}

// run keeps only the connection; the library is kept by the instance's group.
func (i *Instance) run(ctx context.Context) {
	// Waited for, so nothing writes to the database once the instance has stopped.
	var wg sync.WaitGroup
	defer wg.Wait()

	if i.Waker.MAC() == nil {
		wg.Go(func() { i.learnMAC(ctx) })
	}

	if err := i.Client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		i.log.Error("kodi connection stopped", "error", err)
	}
}

func (i *Instance) learnMAC(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(learnMACDelay):
	}

	i.saveMAC(ctx)
}

func (i *Instance) saveMAC(ctx context.Context) {
	if err := i.Waker.Learn(ctx); err != nil {
		i.log.Debug("couldn't read the MAC address for Wake-on-LAN yet", "error", err)

		return
	}

	// The stored row, not i.Record: it may have been edited since, and i.Record is read concurrently, so it isn't written here. The Waker holds the MAC.
	rec, err := i.db.KodiInstances().Get(ctx, i.id)
	if err == nil {
		rec.MAC = i.Waker.MAC().String()
		err = i.db.KodiInstances().Update(ctx, &rec)
	}

	if err != nil {
		i.log.Warn("saving MAC address", "error", err)

		return
	}

	i.log.Info("learned MAC address for Wake-on-LAN", "mac", rec.MAC)
}

// Callers hold App.mu.
func (i *Instance) target() actions.Target {
	t := actions.Target{Name: i.name, Backend: i.Backend, Matcher: i.Index, Library: i.Catalog}

	if i.Waker.MAC() != nil {
		t.Wake = func(ctx context.Context) error { return i.Waker.WakeAndWait(ctx, wakeBudget) }
	}

	return t
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
