package app

import (
	"context"
	"fmt"
	"sync"
)

// ReloadInstance rebuilds the runtime of the Kodi instance row id, as saved through the admin API, so the change takes effect without a restart. A new row gets a runtime of its own; the instance's library group is rebuilt around the new runtime, keeping its title index warm.
func (a *App) ReloadInstance(ctx context.Context, id int64) error {
	rec, err := a.db.KodiInstances().Get(ctx, id)
	if err != nil {
		return err
	}

	inst, err := a.newInstance(ctx, rec)
	if err != nil {
		return fmt.Errorf("kodi instance %q: %w", rec.Name, err)
	}

	a.planMu.Lock()
	old := a.swapInstance(id, inst)
	err = a.plan(ctx)
	a.startInstance(inst)
	a.planMu.Unlock()

	// Waited for outside planMu, which the old runtime's detection may be waiting for.
	old.stop()

	return err
}

// RemoveInstance stops the runtime of a deleted Kodi instance and drops it from its library group.
func (a *App) RemoveInstance(ctx context.Context, id int64) error {
	a.planMu.Lock()
	old := a.swapInstance(id, nil)
	err := a.plan(ctx)
	a.planMu.Unlock()

	old.stop()

	a.mu.Lock()
	delete(a.prints, id)
	a.mu.Unlock()

	return err
}

// swapInstance replaces (or, with nil, removes) the runtime for id and cancels the old one, which the caller stops once it no longer holds planMu. Callers hold planMu.
func (a *App) swapInstance(id int64, inst *Instance) *Instance {
	a.mu.Lock()
	old := a.instances[id]

	if inst == nil {
		delete(a.instances, id)
	} else {
		a.instances[id] = inst
	}
	a.mu.Unlock()

	if old != nil && old.cancel != nil {
		old.cancel()
	}

	return old
}

// startInstance is a no-op until Run starts. Callers hold planMu.
func (a *App) startInstance(inst *Instance) {
	if a.runCtx == nil {
		return
	}

	ctx, cancel := context.WithCancel(a.runCtx)
	inst.cancel, inst.done = cancel, make(chan struct{})

	go func() {
		defer close(inst.done)

		var wg sync.WaitGroup

		wg.Go(func() { inst.run(ctx, a.tune.learnMACDelay) })
		wg.Go(func() { a.watchInstance(ctx, inst) })
		wg.Wait()
	}()
}

// stop cancels the instance's runtime and waits for it. It is safe on nil and on an instance that never ran.
func (i *Instance) stop() {
	if i == nil || i.cancel == nil {
		return
	}

	i.cancel()
	<-i.done
}
