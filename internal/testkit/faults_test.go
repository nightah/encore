package testkit_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
	"github.com/nightah/encore/internal/testkit"
)

func openFaulty(t *testing.T) (*store.DB, *testkit.Faults) {
	t.Helper()

	path := filepath.Join(t.TempDir(), store.FileName)

	db, err := store.Open(t.Context(), store.Options{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	return db, testkit.NewFaults(t, path)
}

func TestFaultsFailTheNthWriteAndEveryOneAfter(t *testing.T) {
	db, faults := openFaulty(t)
	ctx := t.Context()

	faults.Inject(testkit.Fault{Op: testkit.Insert, Table: "rooms", Nth: 2})

	require.NoError(t, db.Rooms().Create(ctx, &store.Room{Name: "Lounge"}))
	require.ErrorContains(t, db.Rooms().Create(ctx, &store.Room{Name: "Study"}), testkit.FaultMessage+" on INSERT rooms")
	require.ErrorContains(t, db.Rooms().Create(ctx, &store.Room{Name: "Attic"}), testkit.FaultMessage, "the database stays broken")
	require.NoError(t, db.Settings().Set(ctx, "a", "b"), "other tables are unaffected")

	rooms, err := db.Rooms().List(ctx)
	require.NoError(t, err)
	assert.Len(t, rooms, 1, "a failed insert leaves nothing behind")

	faults.Clear()
	require.NoError(t, db.Rooms().Create(ctx, &store.Room{Name: "Study"}))
}

func TestFaultsFailOnlyTheRowsThatMeetTheCondition(t *testing.T) {
	db, faults := openFaulty(t)
	ctx := t.Context()

	lounge := store.Room{Name: "Lounge"}
	require.NoError(t, db.Rooms().Create(ctx, &lounge))

	faults.Inject(testkit.Fault{Op: testkit.Update, Table: "rooms", When: "NEW.name = 'Den'"}, testkit.Fault{Op: testkit.Delete, Table: "rooms"})

	lounge.Name = "Den"
	require.ErrorContains(t, db.Rooms().Update(ctx, &lounge), testkit.FaultMessage)

	lounge.Name = "Living room"
	require.NoError(t, db.Rooms().Update(ctx, &lounge))
	require.ErrorContains(t, db.Rooms().Delete(ctx, lounge.ID), testkit.FaultMessage+" on DELETE rooms")
}

func TestFaultsFailTheNthRead(t *testing.T) {
	db, faults := openFaulty(t)
	ctx := t.Context()

	faults.Inject(testkit.Fault{Op: testkit.Read, Table: "rooms", Nth: 2})

	require.ErrorIs(t, db.Rooms().Delete(ctx, 1), store.ErrNotFound, "a write's own reads aren't counted")

	_, err := db.Rooms().List(ctx)
	require.NoError(t, err)

	_, err = db.Rooms().List(ctx)
	require.ErrorContains(t, err, testkit.ReadFaultMessage+": access to rooms.id is prohibited")

	_, err = db.KodiInstances().List(ctx)
	require.NoError(t, err, "other tables are unaffected")

	faults.Clear()

	_, err = db.Rooms().List(ctx)
	require.NoError(t, err)
}
