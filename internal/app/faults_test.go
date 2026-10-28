package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
	"github.com/nightah/encore/internal/testkit"
)

// faultyConfig returns a config whose data directory already holds a database, with a fault injector for it, so New can be failed part way through.
func faultyConfig(t *testing.T, kodis ...config.KodiInstance) (*config.Config, *testkit.Faults) {
	t.Helper()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Kodi = kodis

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(cfg.DataDir)})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	return &cfg, testkit.NewFaults(t, store.DefaultPath(cfg.DataDir))
}

// inspectDB opens the data directory's database after New has failed and closed it. The caller closes it.
func inspectDB(t *testing.T, cfg *config.Config) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(cfg.DataDir)})
	require.NoError(t, err)

	return db
}

func instanceNames(t *testing.T, db *store.DB) []string {
	t.Helper()

	list, err := db.KodiInstances().List(t.Context())
	require.NoError(t, err)

	names := []string{}
	for _, k := range list {
		names = append(names, k.Name)
	}

	return names
}

func secretIDs(t *testing.T, cfg *config.Config, db *store.DB, ids ...string) []string {
	t.Helper()

	key, err := secrets.ReadKey(secrets.Options{DataDir: cfg.DataDir})
	require.NoError(t, err)

	sec, err := secrets.OpenWithKey(db.SecretRows(), key)
	require.NoError(t, err)

	defer sec.Close()

	var found []string

	for _, id := range ids {
		if _, err := sec.Get(t.Context(), id); err == nil {
			found = append(found, id)
		} else {
			require.ErrorIs(t, err, store.ErrNotFound)
		}
	}

	return found
}

func TestNewReportsDatabaseFailuresWhileApplyingTheConfig(t *testing.T) {
	lounge := config.KodiInstance{Name: "Lounge", URL: "http://127.0.0.1:1/jsonrpc", Password: "correct horse", Default: true}
	bedroom := config.KodiInstance{Name: "Bedroom", URL: "http://127.0.0.1:2/jsonrpc", Password: "hunter2"}
	grouped := func(k config.KodiInstance, group string) config.KodiInstance { k.LibraryGroup = group; return k }

	testCases := []struct {
		name  string
		kodi  []config.KodiInstance
		fault testkit.Fault
		// extra faults are injected alongside fault.
		extra []testkit.Fault
		// earlier, when set, is the config of a successful start before the failing one.
		earlier []config.KodiInstance
		// setup prepares the database before the failing start.
		setup func(t *testing.T, db *store.DB)
		want  string
		after func(t *testing.T, cfg *config.Config, db *store.DB)
	}{
		{
			name:  "listing the Kodi instances",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances"},
			want:  "applying kodi instances: querying",
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				assert.Empty(t, instanceNames(t, db))
			},
		},
		{
			name:  "storing a password",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Insert, Table: "secrets"},
			want:  `kodi instance "Lounge": storing password: storing secret "kodi/Lounge/password"`,
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				assert.Empty(t, instanceNames(t, db), "an instance isn't saved without its password")
			},
		},
		{
			name:  "creating the second instance",
			kodi:  []config.KodiInstance{lounge, bedroom},
			fault: testkit.Fault{Op: testkit.Insert, Table: "kodi_instances", Nth: 2},
			want:  `kodi instance "Bedroom": creating kodi instance: `,
			after: func(t *testing.T, cfg *config.Config, db *store.DB) {
				assert.Equal(t, []string{"Lounge"}, instanceNames(t, db), "the instances before it are kept")
				assert.Equal(t, []string{"kodi/Lounge/password"}, secretIDs(t, cfg, db, "kodi/Lounge/password", "kodi/Bedroom/password"),
					"the password of an instance that wasn't saved isn't left behind")
			},
		},
		{
			name:  "removing the password of an instance that wasn't saved",
			kodi:  []config.KodiInstance{bedroom},
			fault: testkit.Fault{Op: testkit.Insert, Table: "kodi_instances"},
			extra: []testkit.Fault{{Op: testkit.Delete, Table: "secrets"}},
			// Both failures are reported.
			want: "creating kodi instance: constraint failed: " + testkit.FaultMessage + " on INSERT kodi_instances (1811)\n" +
				`deleting secret "kodi/Bedroom/password": constraint failed: ` + testkit.FaultMessage + " on DELETE secrets",
		},
		{
			name:    "updating an instance",
			earlier: []config.KodiInstance{bedroom},
			kodi:    []config.KodiInstance{bedroom},
			fault:   testkit.Fault{Op: testkit.Update, Table: "kodi_instances"},
			want:    `kodi instance "Bedroom": updating kodi instance`,
			after: func(t *testing.T, cfg *config.Config, db *store.DB) {
				assert.Equal(t, []string{"Bedroom"}, instanceNames(t, db))
				assert.Equal(t, []string{"kodi/Bedroom/password"}, secretIDs(t, cfg, db, "kodi/Bedroom/password"), "the saved instance keeps its password")
			},
		},
		{
			name:    "giving an instance a password",
			earlier: []config.KodiInstance{{Name: "Bedroom", URL: bedroom.URL}},
			kodi:    []config.KodiInstance{bedroom},
			fault:   testkit.Fault{Op: testkit.Update, Table: "kodi_instances"},
			want:    `kodi instance "Bedroom": updating kodi instance`,
			after: func(t *testing.T, cfg *config.Config, db *store.DB) {
				assert.Empty(t, secretIDs(t, cfg, db, "kodi/Bedroom/password"), "a password the saved instance doesn't use isn't left behind")
			},
		},
		{
			name:  "making an instance the default",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Update, Table: "kodi_instances", When: "NEW.is_default_for_unlinked = 1"},
			want:  `kodi instance "Lounge": setting default kodi instance`,
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				_, err := db.KodiInstances().Default(t.Context())
				require.ErrorIs(t, err, store.ErrNotFound, "no instance is half made the default")
			},
		},
		{
			name:  "listing the Kodi instances for their library groups",
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances"},
			want:  "applying library groups: querying",
		},
		{
			name:  "listing the library groups",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "library_groups"},
			want:  "applying library groups: querying",
		},
		{
			name:  "saving a configured library group",
			kodi:  []config.KodiInstance{grouped(lounge, "Whole house"), grouped(bedroom, "Whole house")},
			fault: testkit.Fault{Op: testkit.Insert, Table: "library_groups"},
			want:  `applying library groups: library group "Whole house": creating library group: `,
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				groups, err := db.LibraryGroups().List(t.Context())
				require.NoError(t, err)
				assert.Empty(t, groups)
			},
		},
		{
			name:  "keeping an instance out of every library group",
			kodi:  []config.KodiInstance{grouped(lounge, config.LibraryGroupNone)},
			fault: testkit.Fault{Op: testkit.Insert, Table: "library_groups"},
			want:  `applying library groups: kodi instance "Lounge": creating library group: `,
		},
		{
			name:    "dissolving a library group the config no longer declares",
			earlier: []config.KodiInstance{grouped(lounge, "Whole house"), grouped(bedroom, "Whole house")},
			kodi:    []config.KodiInstance{lounge, bedroom},
			fault:   testkit.Fault{Op: testkit.Delete, Table: "library_groups"},
			want:    "applying library groups: deleting library group",
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				groups, err := db.LibraryGroups().List(t.Context())
				require.NoError(t, err)
				require.Len(t, groups, 1, "the group is kept until it can be dissolved")
				assert.Equal(t, "Whole house", groups[0].Name)
			},
		},
		{
			name:  "loading the Kodi instances",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances", Nth: 3},
			want:  "loading kodi instances: querying",
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				assert.Equal(t, []string{"Lounge"}, instanceNames(t, db), "the config was applied before the runtimes were prepared")
			},
		},
		{
			name:  "listing the Kodi instances to reconcile their library groups",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances", Nth: 4},
			want:  "reconciling library groups: querying",
		},
		{
			name:  "listing the library groups to reconcile them",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "library_groups", Nth: 2},
			want:  "reconciling library groups: querying",
		},
		{
			name:  "giving an instance a library of its own",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Insert, Table: "library_groups"},
			want:  `reconciling library groups: kodi instance "Lounge": creating library group: `,
		},
		{
			name:    "correcting a library group",
			earlier: []config.KodiInstance{grouped(lounge, "Whole house"), grouped(bedroom, "Whole house")},
			kodi:    []config.KodiInstance{grouped(lounge, "Whole house"), bedroom},
			// The config file's group shrinks to one member, so reconciling makes it solo.
			fault: testkit.Fault{Op: testkit.Update, Table: "library_groups", When: "NEW.state = 'solo'"},
			want:  "reconciling library groups: updating library group",
		},
		{
			name: "removing empty library groups",
			kodi: []config.KodiInstance{lounge},
			setup: func(t *testing.T, db *store.DB) {
				require.NoError(t, db.LibraryGroups().Save(t.Context(), &store.LibraryGroup{Name: "Abandoned"}, nil))
			},
			fault: testkit.Fault{Op: testkit.Delete, Table: "library_groups"},
			want:  "reconciling library groups: deleting empty library groups",
			after: func(t *testing.T, _ *config.Config, db *store.DB) {
				groups, err := db.LibraryGroups().List(t.Context())
				require.NoError(t, err)
				require.Len(t, groups, 2, "the empty group stays until it can be removed")
			},
		},
		{
			name:  "listing the library groups to start them",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "library_groups", Nth: 3},
			want:  "rebuilding library groups: querying",
		},
		{
			name:  "listing the Kodi instances to start their library groups",
			kodi:  []config.KodiInstance{lounge},
			fault: testkit.Fault{Op: testkit.Read, Table: "kodi_instances", Nth: 6},
			want:  "rebuilding library groups: querying",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, faults := faultyConfig(t, tc.earlier...)

			if tc.earlier != nil {
				a, err := New(t.Context(), cfg, slog.New(slog.DiscardHandler))
				require.NoError(t, err)
				require.NoError(t, a.Close())
			}

			if tc.setup != nil {
				db := inspectDB(t, cfg)
				tc.setup(t, db)
				require.NoError(t, db.Close())
			}

			cfg.Kodi = tc.kodi
			faults.Inject(append([]testkit.Fault{tc.fault}, tc.extra...)...)

			_, err := New(t.Context(), cfg, slog.New(slog.DiscardHandler))
			require.ErrorContains(t, err, tc.want)

			if tc.fault.Op == testkit.Read {
				require.ErrorContains(t, err, testkit.ReadFaultMessage)
			} else {
				require.ErrorContains(t, err, testkit.FaultMessage)
			}

			faults.Clear()

			if tc.after != nil {
				db := inspectDB(t, cfg)
				tc.after(t, cfg, db)
				require.NoError(t, db.Close())
			}

			// New closed everything it opened, and the config is applied in full once the database works again.
			a, err := New(t.Context(), cfg, slog.New(slog.DiscardHandler))
			require.NoError(t, err)

			t.Cleanup(func() { assert.NoError(t, a.Close()) })

			want := []string{}
			for _, k := range tc.kodi {
				want = append(want, k.Name)
			}

			assert.ElementsMatch(t, want, instanceNames(t, a.DB()))
			assert.Len(t, a.InstanceIDs(), len(tc.kodi), "every instance has a runtime")
		})
	}
}
