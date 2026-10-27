package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

func newKodi(name string) *KodiInstance {
	return &KodiInstance{Name: name, Scheme: "http", Host: name + ".lan", Port: 8080, Path: "/jsonrpc", WSPort: 9090}
}

func TestKodiInstancesCRUD(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.KodiInstances()

	k := newKodi("lounge")
	k.Username = "kodi"
	k.PasswordSecretRef = "kodi/1/password"
	k.MAC = "aa:bb:cc:dd:ee:ff"
	k.Language, k.Timezone, k.ShutdownQuits = "de", "Australia/Melbourne", new(true)
	require.NoError(t, repo.Create(ctx, k))
	assert.NotZero(t, k.ID)
	assert.Equal(t, clk.Now(), k.CreatedAt)

	got, err := repo.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, *k, got)

	clk.Advance(time.Minute)

	k.Port = 8081
	k.ShutdownQuits = nil
	require.NoError(t, repo.Update(ctx, k))

	got, err = repo.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, 8081, got.Port)
	assert.Nil(t, got.ShutdownQuits, "nil follows alexa.shutdown_quits")
	assert.Equal(t, clk.Now(), got.UpdatedAt)
	assert.True(t, got.UpdatedAt.After(got.CreatedAt))

	err = repo.Create(ctx, newKodi("LOUNGE"))
	require.ErrorIs(t, err, ErrConflict, "names are unique case-insensitively")

	bad := newKodi("bad")
	bad.Port = 0
	require.ErrorIs(t, repo.Create(ctx, bad), ErrInvalid)

	require.NoError(t, repo.Delete(ctx, k.ID))
	_, err = repo.Get(ctx, k.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, repo.Delete(ctx, k.ID), ErrNotFound)
	require.ErrorIs(t, repo.Update(ctx, k), ErrNotFound)
}

func TestKodiInstancesSingleDefault(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.KodiInstances()

	_, err := repo.Default(ctx)
	require.ErrorIs(t, err, ErrNotFound)

	a, b := newKodi("a"), newKodi("b")
	a.IsDefaultForUnlinked = true
	require.NoError(t, repo.Create(ctx, a))

	b.IsDefaultForUnlinked = true
	require.NoError(t, repo.Create(ctx, b))

	def, err := repo.Default(ctx)
	require.NoError(t, err)
	assert.Equal(t, b.ID, def.ID, "creating a new default replaces the old one")

	require.NoError(t, repo.SetDefault(ctx, a.ID))

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.True(t, list[0].IsDefaultForUnlinked)
	assert.False(t, list[1].IsDefaultForUnlinked)

	require.NoError(t, repo.SetDefault(ctx, 0))
	_, err = repo.Default(ctx)
	require.ErrorIs(t, err, ErrNotFound)

	require.ErrorIs(t, repo.SetDefault(ctx, 999), ErrNotFound)

	// The partial unique index is the last line of defence.
	_, err = db.write.ExecContext(ctx, `UPDATE kodi_instances SET is_default_for_unlinked = 1`)
	require.Error(t, err)
}

func TestEchoDevicesSeen(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.EchoDevices()

	first := clk.Now()

	d, err := repo.Seen(ctx, "amzn1.ask.device.X")
	require.NoError(t, err)
	assert.EqualValues(t, 1, d.RequestCount)
	assert.Equal(t, first, d.FirstSeen)
	assert.Equal(t, first, d.LastSeen)

	clk.Advance(time.Hour)

	d, err = repo.Seen(ctx, "amzn1.ask.device.X")
	require.NoError(t, err)
	assert.EqualValues(t, 2, d.RequestCount)
	assert.Equal(t, first, d.FirstSeen)
	assert.Equal(t, clk.Now(), d.LastSeen)

	_, err = repo.Seen(ctx, "")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestEchoDevicesLinksClearedOnDelete(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	k := newKodi("lounge")
	require.NoError(t, db.KodiInstances().Create(ctx, k))

	room := &Room{Name: "Lounge"}
	require.NoError(t, db.Rooms().Create(ctx, room))

	d, err := db.EchoDevices().Seen(ctx, "dev")
	require.NoError(t, err)

	d.DisplayName = "Lounge Echo"
	d.RoomID = room.ID
	d.KodiInstanceID = k.ID
	require.NoError(t, db.EchoDevices().Update(ctx, &d))

	got, err := db.EchoDevices().Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, "Lounge Echo", got.DisplayName)
	assert.Equal(t, room.ID, got.RoomID)
	assert.Equal(t, k.ID, got.KodiInstanceID)

	d.RoomID = 999
	require.ErrorIs(t, db.EchoDevices().Update(ctx, &d), ErrInvalid, "foreign keys are enforced")

	require.NoError(t, db.KodiInstances().Delete(ctx, k.ID))
	require.NoError(t, db.Rooms().Delete(ctx, room.ID))

	got, err = db.EchoDevices().Get(ctx, d.ID)
	require.NoError(t, err)
	assert.Zero(t, got.RoomID)
	assert.Zero(t, got.KodiInstanceID)

	list, err := db.EchoDevices().List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, db.EchoDevices().Delete(ctx, d.ID))
	require.ErrorIs(t, db.EchoDevices().Delete(ctx, d.ID), ErrNotFound)
}

func TestRoomsAndAliases(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Rooms()

	lounge := &Room{Name: "Living Room"}
	require.NoError(t, repo.Create(ctx, lounge))

	bed := &Room{Name: "Kid's Room"}
	require.NoError(t, repo.Create(ctx, bed))

	require.ErrorIs(t, repo.Create(ctx, &Room{Name: "living-room"}), ErrConflict)
	require.ErrorIs(t, repo.Create(ctx, &Room{Name: "!!"}), ErrInvalid)

	a, err := repo.AddAlias(ctx, lounge.ID, "The Lounge")
	require.NoError(t, err)
	assert.Equal(t, "the lounge", a.Normalised)

	_, err = repo.AddAlias(ctx, bed.ID, "the  LOUNGE")
	require.ErrorIs(t, err, ErrConflict, "an alias can name only one room")

	_, err = repo.AddAlias(ctx, bed.ID, "living room")
	require.ErrorIs(t, err, ErrConflict, "an alias cannot shadow another room's name")

	bed.Name = "the lounge"
	require.ErrorIs(t, repo.Update(ctx, bed), ErrConflict, "a name cannot shadow another room's alias")

	got, err := repo.Resolve(ctx, "the lounge")
	require.NoError(t, err)
	assert.Equal(t, lounge.ID, got.ID)

	got, err = repo.Resolve(ctx, "kids room")
	require.NoError(t, err)
	assert.Equal(t, "Kid's Room", got.Name)

	_, err = repo.Resolve(ctx, "garage")
	require.ErrorIs(t, err, ErrNotFound)

	aliases, err := repo.Aliases(ctx, lounge.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 1)

	require.NoError(t, repo.RemoveAlias(ctx, a.ID))
	require.ErrorIs(t, repo.RemoveAlias(ctx, a.ID), ErrNotFound)

	_, err = repo.AddAlias(ctx, lounge.ID, "front room")
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, lounge.ID))

	_, err = repo.Resolve(ctx, "front room")
	require.ErrorIs(t, err, ErrNotFound, "aliases are deleted with their room")

	rooms, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, rooms, 1)
}

func TestSettings(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Settings()

	type matcher struct {
		Threshold float64 `json:"threshold"`
	}

	v, err := Setting(ctx, repo, "matcher", matcher{Threshold: 0.8})
	require.NoError(t, err)
	assert.InDelta(t, 0.8, v.Threshold, 0)

	require.NoError(t, repo.Set(ctx, "matcher", matcher{Threshold: 0.6}))
	require.NoError(t, repo.Set(ctx, "matcher", matcher{Threshold: 0.7}))

	v, err = Setting(ctx, repo, "matcher", matcher{})
	require.NoError(t, err)
	assert.InDelta(t, 0.7, v.Threshold, 0)

	var n int
	require.ErrorIs(t, repo.Get(ctx, "missing", &n), ErrNotFound)
	require.Error(t, repo.Get(ctx, "matcher", &n), "type mismatch is reported")

	all, err := repo.All(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"threshold":0.7}`, string(all["matcher"]))

	require.NoError(t, repo.Delete(ctx, "matcher"))
	require.NoError(t, repo.Delete(ctx, "matcher"))

	_, err = repo.Raw(ctx, "matcher")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTitleAliases(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Aliases()

	k := newKodi("lounge")
	require.NoError(t, db.KodiInstances().Create(ctx, k))

	movie := media.Ref{Kind: media.KindMovie, ID: 42}
	a := &TitleAlias{KodiInstanceID: k.ID, Media: movie, Alias: "The Bad Guy Film"}
	require.NoError(t, repo.Add(ctx, a))
	assert.Equal(t, "the bad guy film", a.Normalised)

	require.ErrorIs(t, repo.Add(ctx, &TitleAlias{KodiInstanceID: k.ID, Media: movie, Alias: "the bad-guy film"}), ErrConflict)
	require.NoError(t, repo.Add(ctx, &TitleAlias{KodiInstanceID: k.ID, Media: media.Ref{Kind: media.KindTVShow, ID: 7}, Alias: "the bad guy film"}))
	require.ErrorIs(t, repo.Add(ctx, &TitleAlias{KodiInstanceID: 999, Media: movie, Alias: "x"}), ErrInvalid)

	found, err := repo.Lookup(ctx, k.ID, "THE BAD GUY FILM")
	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.Equal(t, movie, found[0].Media)

	forMovie, err := repo.ForMedia(ctx, k.ID, movie)
	require.NoError(t, err)
	assert.Len(t, forMovie, 1)

	all, err := repo.List(ctx, k.ID)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	require.NoError(t, repo.Delete(ctx, a.ID))
	require.ErrorIs(t, repo.Delete(ctx, a.ID), ErrNotFound)

	require.NoError(t, db.KodiInstances().Delete(ctx, k.ID))

	all, err = repo.List(ctx, k.ID)
	require.NoError(t, err)
	assert.Empty(t, all, "aliases are deleted with their instance")
}

func TestRequestLogRetentionByCount(t *testing.T) {
	db, clk := openTest(t, Options{RequestLog: RequestLogRetention{MaxEntries: 3}})
	ctx := t.Context()
	repo := db.RequestLog()

	for i := range 5 {
		clk.Advance(time.Second)
		require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: string(rune('a' + i))}))
	}

	list, err := repo.List(ctx, RequestLogQuery{})
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, []string{"e", "d", "c"}, []string{list[0].RequestID, list[1].RequestID, list[2].RequestID})

	page, err := repo.List(ctx, RequestLogQuery{Limit: 1, BeforeID: list[0].ID})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "d", page[0].RequestID)
}

func TestRequestLogRetentionByAge(t *testing.T) {
	db, clk := openTest(t, Options{RequestLog: RequestLogRetention{MaxAge: time.Hour}})
	ctx := t.Context()
	repo := db.RequestLog()

	require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: "old"}))
	clk.Advance(2 * time.Hour)
	require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: "new"}))

	list, err := repo.List(ctx, RequestLogQuery{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "new", list[0].RequestID)

	clk.Advance(2 * time.Hour)
	require.NoError(t, repo.Prune(ctx))

	list, err = repo.List(ctx, RequestLogQuery{})
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRequestLogRoundTripAndSubscribe(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.RequestLog()

	var got []RequestLogEntry

	cancel := repo.Subscribe(func(e RequestLogEntry) { got = append(got, e) })

	e := &RequestLogEntry{
		RequestID: "amzn1.echo-api.request.1",
		DeviceID:  "amzn1.ask.device.X",
		Locale:    "en-GB",
		Intent:    "PlayMovieIntent",
		Slots:     json.RawMessage(`{"movie":"alien"}`),
		Match:     json.RawMessage(`{"score":0.93}`),
		KodiCalls: json.RawMessage(`[{"method":"Player.Open"}]`),
		Speech:    "Playing Alien",
		Outcome:   "ok",
		Duration:  123 * time.Millisecond,
	}
	require.NoError(t, repo.Insert(ctx, e))
	assert.Equal(t, clk.Now(), e.Time)

	stored, err := repo.Get(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, *e, stored)

	require.Len(t, got, 1)
	assert.Equal(t, e.ID, got[0].ID)

	cancel()
	require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: "2"}))
	assert.Len(t, got, 1, "cancelled subscribers are not called")

	require.ErrorIs(t, repo.Insert(ctx, &RequestLogEntry{Slots: json.RawMessage(`{bad`)}), ErrInvalid)

	nulls, err := repo.List(ctx, RequestLogQuery{Limit: 1})
	require.NoError(t, err)
	assert.Nil(t, nulls[0].Slots)

	_, err = repo.Get(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRequestLogSubscriberMayCancelItself(t *testing.T) {
	db, _ := openTest(t, Options{})

	var cancel func()

	calls := 0
	cancel = db.RequestLog().Subscribe(func(RequestLogEntry) {
		calls++
		cancel()
	})

	require.NoError(t, db.RequestLog().Insert(t.Context(), &RequestLogEntry{}))
	require.NoError(t, db.RequestLog().Insert(t.Context(), &RequestLogEntry{}))
	assert.Equal(t, 1, calls)
}

func TestSessions(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Sessions()

	s := &Session{IDHash: []byte{1, 2, 3}, Username: "admin", ExpiresAt: clk.Now().Add(time.Hour)}
	require.NoError(t, repo.Create(ctx, s))
	require.ErrorIs(t, repo.Create(ctx, s), ErrConflict)

	got, err := repo.Get(ctx, s.IDHash)
	require.NoError(t, err)
	assert.Equal(t, *s, got)

	clk.Advance(30 * time.Minute)
	require.NoError(t, repo.Touch(ctx, s.IDHash, clk.Now().Add(time.Hour)))

	got, err = repo.Get(ctx, s.IDHash)
	require.NoError(t, err)
	assert.Equal(t, clk.Now(), got.LastSeenAt)

	require.ErrorIs(t, repo.Touch(ctx, []byte{9}, clk.Now()), ErrNotFound)

	clk.Advance(2 * time.Hour)
	_, err = repo.Get(ctx, s.IDHash)
	require.ErrorIs(t, err, ErrNotFound, "expired sessions are not returned")

	n, err := repo.DeleteExpired(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	for _, id := range [][]byte{{4}, {5}} {
		require.NoError(t, repo.Create(ctx, &Session{IDHash: id, Username: "admin", ExpiresAt: clk.Now().Add(time.Hour)}))
	}

	require.NoError(t, repo.Delete(ctx, []byte{4}))

	n, err = repo.DeleteUser(ctx, "admin")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	for _, id := range [][]byte{{6}, {7}, {8}} {
		require.NoError(t, repo.Create(ctx, &Session{IDHash: id, Username: "admin", ExpiresAt: clk.Now().Add(time.Hour)}))
	}

	n, err = repo.DeleteAllExcept(ctx, []byte{7})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)

	_, err = repo.Get(ctx, []byte{7})
	require.NoError(t, err)

	n, err = repo.DeleteAllExcept(ctx, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestSecretRows(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.SecretRows()

	created := clk.Now()
	require.NoError(t, repo.Put(ctx, SecretRow{ID: "a", KeyID: "k1", Nonce: []byte{1}, Ciphertext: []byte{2}}))
	require.NoError(t, repo.Put(ctx, SecretRow{ID: "b", KeyID: "k1", Nonce: []byte{3}, Ciphertext: []byte{4}}))

	clk.Advance(time.Minute)
	require.NoError(t, repo.Put(ctx, SecretRow{ID: "a", KeyID: "k1", Nonce: []byte{5}, Ciphertext: []byte{6}}))

	row, err := repo.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, []byte{6}, row.Ciphertext)
	assert.Equal(t, created, row.CreatedAt)
	assert.Equal(t, clk.Now(), row.UpdatedAt)

	err = repo.Rewrite(ctx, func(r *SecretRow) error {
		r.KeyID = "k2"

		return nil
	})
	require.NoError(t, err)

	ids, err := repo.KeyIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"k2"}, ids)

	err = repo.Rewrite(ctx, func(r *SecretRow) error {
		if r.ID == "b" {
			return assert.AnError
		}

		r.KeyID = "k3"

		return nil
	})
	require.ErrorIs(t, err, assert.AnError)

	ids, err = repo.KeyIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"k2"}, ids, "a failed rewrite changes nothing")

	require.NoError(t, repo.Delete(ctx, "a"))
	_, err = repo.Get(ctx, "a")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRequestLogSimulated(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.RequestLog()

	require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: "sim", DeviceID: "amzn1.ask.device.SIM", Intent: "LaunchRequest", Simulated: true}))
	require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: "echo", DeviceID: "amzn1.ask.device.ECHO", Intent: "LaunchRequest"}))

	got, err := repo.List(ctx, RequestLogQuery{})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.False(t, got[0].Simulated)
	assert.True(t, got[1].Simulated, "kept through the round trip")
}
