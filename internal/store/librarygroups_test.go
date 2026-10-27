package store

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibraryGroupsSaveAndMembers(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	kodis, groups := db.KodiInstances(), db.LibraryGroups()

	a, b, c := newKodi("a"), newKodi("b"), newKodi("c")
	for _, k := range []*KodiInstance{a, b, c} {
		require.NoError(t, kodis.Create(ctx, k))
		assert.Zero(t, k.LibraryGroupID)
	}

	g := &LibraryGroup{Name: "Shared", SourceInstanceID: a.ID, State: GroupLikelyShared, Evidence: json.RawMessage(`{"matched":200}`)}
	require.NoError(t, groups.Save(ctx, g, []int64{a.ID, b.ID}))
	assert.NotZero(t, g.ID)
	assert.Equal(t, clk.Now(), g.CreatedAt)

	got, err := groups.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, *g, got)

	members, err := groups.Members(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{a.ID, b.ID}, members)

	ka, err := kodis.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, g.ID, ka.LibraryGroupID)

	// Updating an instance never touches its group.
	ka.Port = 8081
	require.NoError(t, kodis.Update(ctx, &ka))
	ka, err = kodis.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, g.ID, ka.LibraryGroupID)

	// Saving with a new member list replaces the old one.
	clk.Advance(time.Minute)

	g.State, g.Manual, g.Evidence = GroupManual, true, nil
	require.NoError(t, groups.Save(ctx, g, []int64{b.ID, c.ID}))

	members, err = groups.Members(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{b.ID, c.ID}, members)

	got, err = groups.Get(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, GroupManual, got.State)
	assert.True(t, got.Manual)
	assert.Nil(t, got.Evidence)
	assert.Equal(t, clk.Now(), got.UpdatedAt)

	// A nil member list leaves membership alone.
	g.Name = "Renamed"
	require.NoError(t, groups.Save(ctx, g, nil))

	members, err = groups.Members(ctx, g.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	ka, err = kodis.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.Zero(t, ka.LibraryGroupID, "a left the group")

	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{}, []int64{999}), ErrNotFound)
	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{State: "bogus"}, nil), ErrInvalid)
	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{ID: 999}, nil), ErrNotFound)
}

func TestLibraryGroupsAssignDeleteAndCleanup(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	kodis, groups := db.KodiInstances(), db.LibraryGroups()

	a, b := newKodi("a"), newKodi("b")
	require.NoError(t, kodis.Create(ctx, a))
	require.NoError(t, kodis.Create(ctx, b))

	ga, gb, empty := &LibraryGroup{SourceInstanceID: a.ID}, &LibraryGroup{SourceInstanceID: b.ID}, &LibraryGroup{Name: "empty"}
	require.NoError(t, groups.Save(ctx, ga, []int64{a.ID}))
	require.NoError(t, groups.Save(ctx, gb, []int64{b.ID}))
	require.NoError(t, groups.Save(ctx, empty, []int64{}))
	assert.Equal(t, GroupSolo, ga.State, "the state defaults to solo")

	require.NoError(t, groups.Assign(ctx, b.ID, ga.ID))
	require.ErrorIs(t, groups.Assign(ctx, 999, ga.ID), ErrNotFound)
	require.ErrorIs(t, groups.Assign(ctx, a.ID, 999), ErrInvalid, "the group must exist")

	deleted, err := groups.DeleteEmpty(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{gb.ID, empty.ID}, deleted)

	list, err := groups.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, ga.ID, list[0].ID)

	// IDs are never reused, so a snapshot keyed by a deleted group's ID can't be mistaken for a new group's.
	fresh := &LibraryGroup{}
	require.NoError(t, groups.Save(ctx, fresh, nil))
	assert.Greater(t, fresh.ID, empty.ID)

	// Deleting a source instance clears the reference; deleting a group frees its members.
	require.NoError(t, kodis.Delete(ctx, a.ID))

	got, err := groups.Get(ctx, ga.ID)
	require.NoError(t, err)
	assert.Zero(t, got.SourceInstanceID)

	require.NoError(t, groups.Delete(ctx, ga.ID))
	require.ErrorIs(t, groups.Delete(ctx, ga.ID), ErrNotFound)

	kb, err := kodis.Get(ctx, b.ID)
	require.NoError(t, err)
	assert.Zero(t, kb.LibraryGroupID)
}

// TestLibraryGroupsMigrationOnExistingDatabase upgrades a database created by the first migration, with an instance already in it.
func TestLibraryGroupsMigrationOnExistingDatabase(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), FileName)

	raw, err := sql.Open("sqlite", dsn(path, false))
	require.NoError(t, err)

	initial, err := fs.ReadFile(migrationFiles, "migrations/00001_init.sql")
	require.NoError(t, err)

	p, err := goose.NewProvider(goose.DialectSQLite3, raw, fstest.MapFS{"00001_init.sql": {Data: initial}})
	require.NoError(t, err)

	_, err = p.Up(ctx)
	require.NoError(t, err)

	_, err = raw.ExecContext(ctx, `INSERT INTO kodi_instances (name, host, created_at, updated_at) VALUES ('Lounge', 'lounge.lan', 1, 1)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	db, _ := openTest(t, Options{Path: path})

	list, err := db.KodiInstances().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Zero(t, list[0].LibraryGroupID, "existing instances start without a group")

	g := &LibraryGroup{SourceInstanceID: list[0].ID}
	require.NoError(t, db.LibraryGroups().Save(ctx, g, []int64{list[0].ID}))

	// The migration reverses cleanly.
	fsys, err := fs.Sub(migrationFiles, "migrations")
	require.NoError(t, err)

	down, err := goose.NewProvider(goose.DialectSQLite3, db.write, fsys)
	require.NoError(t, err)

	_, err = down.Down(ctx)
	require.NoError(t, err)

	_, err = down.Up(ctx)
	require.NoError(t, err)
}
