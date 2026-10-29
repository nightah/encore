package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func TestRemoveInstanceLeavesItsGroup(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		lib := testLibrary(30, 0, "2024-01-01 10:00:00")
		lounge, bedroom := newKodi(t, fakekodi.Options{Library: lib}), newKodi(t, fakekodi.Options{Library: lib})
		a, _ := runApp(t, t.TempDir(), fastTuning(), kodiConfig("Lounge", lounge, "upstairs"), kodiConfig("Bedroom", bedroom, "upstairs"))
		ctx := t.Context()
		loungeID, bedroomID := a.instanceNamed(t, "Lounge").id, a.instanceNamed(t, "Bedroom").id

		connected(t, bedroom)

		// The admin API deletes the row, then the runtime.
		require.NoError(t, a.DB().KodiInstances().Delete(ctx, bedroomID))
		require.NoError(t, a.RemoveInstance(ctx, bedroomID))

		assert.Equal(t, []int64{loungeID}, a.InstanceIDs())

		g, ok := a.groupOf("Lounge")
		require.True(t, ok)
		assert.Equal(t, []string{"Lounge"}, memberNames(g))

		_, err := a.InstanceStatus(ctx, bedroomID, false)
		require.ErrorIs(t, err, ErrNoRuntime)

		settled(t, func() bool { return bedroom.WebSocketClients() == 0 }, "the removed runtime disconnects")

		a.mu.RLock()
		_, printed := a.prints[bedroomID]
		a.mu.RUnlock()
		assert.False(t, printed, "its fingerprint is forgotten")

		require.NoError(t, a.RemoveInstance(ctx, bedroomID), "removing twice is harmless")
	})
}

func TestRemovingTheLastMemberDeletesTheSnapshot(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		srv := newKodi(t, fakekodi.Options{})
		dir := t.TempDir()
		a, _ := runApp(t, dir, fastTuning(), kodiConfig("Lounge", srv, ""))
		ctx := t.Context()
		inst := a.instanceNamed(t, "Lounge")

		g, _ := a.groupOf("Lounge")
		file := filepath.Join(dir, "blobs", "library", fmt.Sprintf("%d.json.gz", g.ID))

		after(t, fastTuning().snapshotCheck, func() bool { _, err := os.Stat(file); return err == nil }, "the synced library is saved at the next check")

		require.NoError(t, a.DB().KodiInstances().Delete(ctx, inst.id))
		require.NoError(t, a.RemoveInstance(ctx, inst.id))

		assert.Empty(t, a.Groups())
		assert.NoFileExists(t, file, "a deleted library's snapshot goes with it")
	})
}

func TestReloadInstance(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		ctx := t.Context()
		old := h.instance(t)

		require.ErrorIs(t, h.app.ReloadInstance(ctx, unknownInstance), store.ErrNotFound)

		rec := old.Record
		rec.Timezone = "Australia/Melbourne"
		require.NoError(t, h.app.DB().KodiInstances().Update(ctx, &rec))
		require.NoError(t, h.app.ReloadInstance(ctx, rec.ID))

		inst := h.instance(t)
		assert.NotSame(t, old, inst)
		assert.Equal(t, "Australia/Melbourne", inst.Record.Timezone)
		assert.Positive(t, inst.Index.Len(), "the title index stays warm across the reload")

		select {
		case <-old.done:
		default:
			t.Fatal("the replaced runtime is stopped")
		}

		// A saved password whose secret is missing can't be used.
		rec.PasswordSecretRef = "kodi/Lounge/missing"
		require.NoError(t, h.app.DB().KodiInstances().Update(ctx, &rec))
		require.ErrorContains(t, h.app.ReloadInstance(ctx, rec.ID), "reading password")
		assert.Same(t, inst, h.instance(t), "a runtime that can't be built doesn't replace the running one")
	})
}

func TestNothingConnectsBeforeRun(t *testing.T) {
	lounge, bedroom := newKodi(t, fakekodi.Options{}), newKodi(t, fakekodi.Options{})

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Kodi = []config.KodiInstance{kodiConfig("Lounge", lounge, ""), kodiConfig("Bedroom", bedroom, "")}

	a := openApp(t, &cfg, fastTuning())

	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	ctx := t.Context()
	ids := a.InstanceIDs()
	require.Len(t, ids, 2)

	_, err := a.GroupInstances(ctx, "Whole house", ids...)
	require.NoError(t, err)
	require.NoError(t, a.ReloadInstance(ctx, ids[0]))
	require.NoError(t, a.SplitInstance(ctx, ids[1]))

	assert.Len(t, a.Groups(), 2, "groups are planned before Run")
	assert.Empty(t, lounge.Calls(), "but nothing reaches Kodi until Run")
	assert.Empty(t, bedroom.Calls())
}

func TestNewRejectsUnusableInstances(t *testing.T) {
	for name, c := range map[string]struct {
		kodi config.KodiInstance
		err  string
	}{
		"bad url": {config.KodiInstance{Name: "Lounge", URL: "http://[::1"}, `kodi instance "Lounge"`},
		"bad mac": {config.KodiInstance{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc", MAC: "not-a-mac"}, `parsing MAC "not-a-mac"`},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.DataDir = t.TempDir()
			cfg.Kodi = []config.KodiInstance{c.kodi}

			_, err := New(t.Context(), &cfg, slog.New(slog.DiscardHandler))
			require.ErrorContains(t, err, c.err)
		})
	}
}

func TestNewKeepsInstancesAndDefaultsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	kodis := []config.KodiInstance{{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc", Default: true}, {Name: "Bedroom", URL: "http://127.0.0.1:2/jsonrpc"}}

	open := func() *App {
		cfg := config.Defaults()
		cfg.DataDir = dir
		cfg.Kodi = kodis

		a, err := New(t.Context(), &cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)

		return a
	}

	a := open()
	first := a.InstanceIDs()
	require.NoError(t, a.Close())

	kodis[0].Default, kodis[1].Default = false, true
	kodis[1].URL = "http://127.0.0.1:3/jsonrpc"

	a = open()

	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	assert.Equal(t, first, a.InstanceIDs(), "instances are matched by name, not added again")

	def, err := a.DB().KodiInstances().Default(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bedroom", def.Name)
	assert.Equal(t, 3, def.Port, "a changed URL is applied")
}

func TestSavingTheMACNeedsKodiAndTheRow(t *testing.T) {
	h := newHarness(t, true)
	ctx := t.Context()
	inst := h.instance(t)

	h.kodi.Close()
	inst.saveMAC(ctx)

	assert.Nil(t, inst.Waker.MAC(), "an unreachable Kodi can't tell its MAC address")

	rec, err := h.app.DB().KodiInstances().Get(ctx, inst.id)
	require.NoError(t, err)
	assert.Empty(t, rec.MAC)

	h.kodi.Restart(t)
	require.NoError(t, h.app.DB().KodiInstances().Delete(ctx, inst.id))

	inst.saveMAC(ctx)
	assert.NotNil(t, inst.Waker.MAC(), "the runtime still knows it until it stops")

	_, err = h.app.DB().KodiInstances().Get(ctx, inst.id)
	require.ErrorIs(t, err, store.ErrNotFound, "an instance deleted meanwhile isn't brought back by saving its MAC")
}

func TestTheMACIsLearnedOnceTheConnectionSettles(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		ctx := t.Context()
		inst := h.instance(t)
		delay := defaultTuning().learnMACDelay

		stored := func() string {
			rec, err := h.app.DB().KodiInstances().Get(ctx, inst.id)
			require.NoError(t, err)

			return rec.MAC
		}

		after(t, delay-time.Millisecond, func() bool { return inst.Waker.MAC() == nil && stored() == "" }, "the MAC address isn't asked for while the connection settles")
		assert.Zero(t, macAsks(h.kodi))

		after(t, time.Millisecond, func() bool { return stored() == "dc:a6:32:4b:19:0e" }, "it's read and saved once the delay has passed")
		assert.Equal(t, "dc:a6:32:4b:19:0e", inst.Waker.MAC().String())
	})
}

func TestAnInstanceStoppedEarlyDoesntLearnItsMAC(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		ctx := t.Context()
		inst := h.instance(t)

		synctest.Wait()
		require.NoError(t, h.app.DB().KodiInstances().Delete(ctx, inst.id))

		stopping := time.Now()
		require.NoError(t, h.app.RemoveInstance(ctx, inst.id))
		assert.Zero(t, time.Since(stopping), "stopping doesn't wait out the delay")

		time.Sleep(defaultTuning().learnMACDelay)
		synctest.Wait()
		assert.Nil(t, inst.Waker.MAC(), "a stopped instance stops waiting to learn its MAC address")
		assert.Zero(t, macAsks(h.kodi))
	})
}

// macAsks counts the times a fake was asked for its MAC address.
func macAsks(srv *fakekodi.Server) int {
	n := 0

	for _, c := range srv.CallsTo("XBMC.GetInfoLabels") {
		if strings.Contains(string(c.Params), "Network.MacAddress") {
			n++
		}
	}

	return n
}

func TestTimezone(t *testing.T) {
	assert.Nil(t, timezone(""), "no zone means learn the offset from Kodi")
	assert.Nil(t, timezone("Mars/Olympus_Mons"), "an unknown zone does too")

	loc := timezone("Australia/Melbourne")
	require.NotNil(t, loc)
	assert.Equal(t, "Australia/Melbourne", loc.String())
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "a", firstNonEmpty("", "a", "b"))
	assert.Equal(t, "a", firstNonEmpty("a", ""))
	assert.Empty(t, firstNonEmpty("", ""))
	assert.Empty(t, firstNonEmpty())
}

func TestInstanceLanguageDefaultsToEnglish(t *testing.T) {
	assert.Equal(t, "en", (&Instance{}).language())
	assert.Equal(t, "de", (&Instance{Record: store.KodiInstance{Language: "de"}}).language())
}
