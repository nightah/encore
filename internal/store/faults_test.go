package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/testkit"
)

// Writes that take several statements run in one transaction, so a failure part way through leaves the database as it was.
func TestFailedWritesChangeNothing(t *testing.T) {
	testCases := []struct {
		name  string
		fault testkit.Fault
		// write fails part way through, with an error containing want; check then shows the database is as it was.
		write func(t *testing.T, db *DB) error
		want  string
		check func(t *testing.T, db *DB)
	}{
		{
			name:  "creating the new default Kodi instance",
			fault: testkit.Fault{Op: testkit.Update, Table: "kodi_instances", When: "NEW.is_default_for_unlinked = 0"},
			write: func(t *testing.T, db *DB) error {
				return db.KodiInstances().Create(t.Context(), &KodiInstance{Name: "Bedroom", Scheme: "http", Host: "bedroom", Port: 8080, IsDefaultForUnlinked: true})
			},
			want: "clearing default kodi instance: ",
			check: func(t *testing.T, db *DB) {
				list, err := db.KodiInstances().List(t.Context())
				require.NoError(t, err)
				require.Len(t, list, 1, "the instance isn't created")
				assert.True(t, list[0].IsDefaultForUnlinked, "the old default stays")
			},
		},
		{
			name:  "making an updated Kodi instance the default",
			fault: testkit.Fault{Op: testkit.Update, Table: "kodi_instances", When: "NEW.is_default_for_unlinked = 0"},
			write: func(t *testing.T, db *DB) error {
				other := KodiInstance{Name: "Bedroom", Scheme: "http", Host: "bedroom", Port: 8080}
				require.NoError(t, db.KodiInstances().Create(t.Context(), &other))

				other.Host, other.IsDefaultForUnlinked = "den", true

				return db.KodiInstances().Update(t.Context(), &other)
			},
			want: "clearing default kodi instance: ",
			check: func(t *testing.T, db *DB) {
				def, err := db.KodiInstances().Default(t.Context())
				require.NoError(t, err)
				assert.Equal(t, "Lounge", def.Name)

				other, err := db.KodiInstances().Get(t.Context(), def.ID+1)
				require.NoError(t, err)
				assert.Equal(t, "bedroom", other.Host, "nor is the rest of the update")
			},
		},
		{
			name:  "changing the default Kodi instance",
			fault: testkit.Fault{Op: testkit.Update, Table: "kodi_instances", When: "NEW.is_default_for_unlinked = 0"},
			write: func(t *testing.T, db *DB) error {
				other := KodiInstance{Name: "Bedroom", Scheme: "http", Host: "bedroom", Port: 8080}
				require.NoError(t, db.KodiInstances().Create(t.Context(), &other))

				return db.KodiInstances().SetDefault(t.Context(), other.ID)
			},
			want: "clearing default kodi instance: ",
			check: func(t *testing.T, db *DB) {
				def, err := db.KodiInstances().Default(t.Context())
				require.NoError(t, err)
				assert.Equal(t, "Lounge", def.Name)
			},
		},
		{
			name:  "renaming a library group and replacing its members",
			fault: testkit.Fault{Op: testkit.Update, Table: "kodi_instances", When: "NEW.library_group_id IS NULL"},
			write: func(t *testing.T, db *DB) error {
				lounge, err := db.KodiInstances().Default(t.Context())
				require.NoError(t, err)

				g := LibraryGroup{Name: "Whole house", SourceInstanceID: lounge.ID}
				require.NoError(t, db.LibraryGroups().Save(t.Context(), &g, nil))
				require.NoError(t, db.LibraryGroups().Assign(t.Context(), lounge.ID, g.ID))

				g.Name = "Downstairs"

				return db.LibraryGroups().Save(t.Context(), &g, []int64{lounge.ID})
			},
			want: "clearing members of library group 1: ",
			check: func(t *testing.T, db *DB) {
				g, err := db.LibraryGroups().Get(t.Context(), 1)
				require.NoError(t, err)
				assert.Equal(t, "Whole house", g.Name)

				members, err := db.LibraryGroups().Members(t.Context(), 1)
				require.NoError(t, err)
				assert.Len(t, members, 1)
			},
		},
		{
			name:  "creating a room whose name can't be checked",
			fault: testkit.Fault{Op: testkit.Read, Table: "room_aliases"},
			write: func(t *testing.T, db *DB) error {
				return db.Rooms().Create(t.Context(), &Room{Name: "Kitchen"})
			},
			want: "checking room name: ",
			check: func(t *testing.T, db *DB) {
				rooms, err := db.Rooms().List(t.Context())
				require.NoError(t, err)
				assert.Empty(t, rooms)
			},
		},
		{
			name:  "re-encrypting secrets that can't be read",
			fault: testkit.Fault{Op: testkit.Read, Table: "secrets"},
			write: func(t *testing.T, db *DB) error {
				require.NoError(t, db.SecretRows().Put(t.Context(), SecretRow{ID: "a", KeyID: "old", Nonce: []byte{1}, Ciphertext: []byte{2}}))

				return db.SecretRows().Rewrite(t.Context(), func(row *SecretRow) error {
					row.KeyID = "new"

					return nil
				})
			},
			want: "querying: ",
			check: func(t *testing.T, db *DB) {
				ids, err := db.SecretRows().KeyIDs(t.Context())
				require.NoError(t, err)
				assert.Equal(t, []string{"old"}, ids)
			},
		},
		{
			name:  "re-encrypting the second secret",
			fault: testkit.Fault{Op: testkit.Update, Table: "secrets", Nth: 2},
			write: func(t *testing.T, db *DB) error {
				for _, id := range []string{"a", "b"} {
					require.NoError(t, db.SecretRows().Put(t.Context(), SecretRow{ID: id, KeyID: "old", Nonce: []byte{1}, Ciphertext: []byte{2}}))
				}

				return db.SecretRows().Rewrite(t.Context(), func(row *SecretRow) error {
					row.KeyID, row.Ciphertext = "new", []byte{3}

					return nil
				})
			},
			want: `rewriting secret "b": `,
			check: func(t *testing.T, db *DB) {
				ids, err := db.SecretRows().KeyIDs(t.Context())
				require.NoError(t, err)
				assert.Equal(t, []string{"old"}, ids, "the first secret's rewrite is undone too")

				row, err := db.SecretRows().Get(t.Context(), "a")
				require.NoError(t, err)
				assert.Equal(t, []byte{2}, row.Ciphertext)
			},
		},
		{
			name:  "logging a request that can't be pruned",
			fault: testkit.Fault{Op: testkit.Delete, Table: "request_log"},
			write: func(t *testing.T, db *DB) error {
				db.retention.MaxEntries = 1
				require.NoError(t, db.RequestLog().Insert(t.Context(), &RequestLogEntry{RequestID: "first"}), "nothing to prune yet")

				return db.RequestLog().Insert(t.Context(), &RequestLogEntry{RequestID: "second"})
			},
			want: "logging request: pruning request log: ",
			check: func(t *testing.T, db *DB) {
				entries, err := db.RequestLog().List(t.Context(), RequestLogQuery{})
				require.NoError(t, err)
				require.Len(t, entries, 1, "the request isn't logged without pruning")
				assert.Equal(t, "first", entries[0].RequestID)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openTest(t, Options{})
			require.NoError(t, db.KodiInstances().Create(t.Context(), &KodiInstance{Name: "Lounge", Scheme: "http", Host: "lounge", Port: 8080, IsDefaultForUnlinked: true}))

			faults := testkit.NewFaults(t, db.Path())
			faults.Inject(tc.fault)

			err := tc.write(t, db)
			require.ErrorContains(t, err, tc.want)

			if tc.fault.Op == testkit.Read {
				require.ErrorContains(t, err, testkit.ReadFaultMessage)
			} else {
				require.ErrorContains(t, err, testkit.FaultMessage)
			}

			faults.Clear()
			tc.check(t, db)
		})
	}
}

func TestSettingsReportReadFailures(t *testing.T) {
	db, _ := openTest(t, Options{})
	require.NoError(t, db.Settings().Set(t.Context(), "voice.content", []string{"movies"}))

	testkit.NewFaults(t, db.Path()).Inject(testkit.Fault{Op: testkit.Read, Table: "settings"})

	_, err := db.Settings().All(t.Context())
	require.ErrorContains(t, err, "listing settings: "+testkit.ReadFaultMessage)
}
