package store

import (
	"context"
	"encoding/json"
	"strconv"
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

	a, err := repo.AddAlias(ctx, lounge.ID, "The Lounge", SpokenRoom)
	require.NoError(t, err)
	assert.Equal(t, "the lounge", a.Normalised)

	_, err = repo.AddAlias(ctx, bed.ID, "the  LOUNGE", SpokenRoom)
	require.ErrorIs(t, err, ErrConflict, "an alias can name only one room")

	_, err = repo.AddAlias(ctx, bed.ID, "living room", SpokenRoom)
	require.ErrorIs(t, err, ErrConflict, "an alias cannot shadow another room's name")

	bed.Name = "the lounge"
	require.ErrorIs(t, repo.Update(ctx, bed), ErrConflict, "a name cannot shadow another room's alias")

	got, kind, err := repo.Resolve(ctx, "the lounge")
	require.NoError(t, err)
	assert.Equal(t, lounge.ID, got.ID)
	assert.Equal(t, SpokenRoom, kind)

	got, kind, err = repo.Resolve(ctx, "kids room")
	require.NoError(t, err)
	assert.Equal(t, "Kid's Room", got.Name)
	assert.Equal(t, SpokenRoom, kind, "a room's own name is a room name")

	_, _, err = repo.Resolve(ctx, "garage")
	require.ErrorIs(t, err, ErrNotFound)

	tv, err := repo.AddAlias(ctx, lounge.ID, "TV", SpokenDevice)
	require.NoError(t, err)

	got, kind, err = repo.Resolve(ctx, "tv")
	require.NoError(t, err)
	assert.Equal(t, lounge.ID, got.ID, "a device resolves to its room")
	assert.Equal(t, SpokenDevice, kind)

	_, err = repo.AddAlias(ctx, lounge.ID, "hall", "speaker")
	require.ErrorIs(t, err, ErrInvalid)

	aliases, err := repo.Aliases(ctx, lounge.ID)
	require.NoError(t, err)
	require.Len(t, aliases, 2)
	assert.Equal(t, SpokenRoom, aliases[0].Kind)
	assert.Equal(t, SpokenDevice, aliases[1].Kind)

	require.NoError(t, repo.RemoveAlias(ctx, tv.ID))

	require.NoError(t, repo.RemoveAlias(ctx, a.ID))
	require.ErrorIs(t, repo.RemoveAlias(ctx, a.ID), ErrNotFound)

	_, err = repo.AddAlias(ctx, lounge.ID, "front room", SpokenRoom)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, lounge.ID))

	_, _, err = repo.Resolve(ctx, "front room")
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

func TestRequestLogDefaults(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.RequestLog()

	assert.Equal(t, RequestLogRetention{MaxEntries: DefaultRequestLogMaxEntries, MaxAge: DefaultRequestLogMaxAge}, db.retention, "unset limits take the defaults")

	for i := range 101 {
		require.NoError(t, repo.Insert(ctx, &RequestLogEntry{RequestID: strconv.Itoa(i)}))
	}

	list, err := repo.List(ctx, RequestLogQuery{})
	require.NoError(t, err)
	require.Len(t, list, 100, "a page holds 100 entries unless asked otherwise")
	assert.Equal(t, "100", list[0].RequestID, "newest first")
	assert.Equal(t, "1", list[99].RequestID)

	list, err = repo.List(ctx, RequestLogQuery{Limit: -1, BeforeID: -5})
	require.NoError(t, err)
	assert.Len(t, list, 100, "negative values mean the defaults too")
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

func TestSessionsExpireAtTheirExpiryInstant(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Sessions()

	require.NoError(t, repo.Create(ctx, &Session{IDHash: []byte{1}, Username: "admin", ExpiresAt: clk.Now().Add(time.Hour)}))

	clk.Advance(time.Hour - time.Millisecond)

	_, err := repo.Get(ctx, []byte{1})
	require.NoError(t, err, "a session is valid until its last millisecond")

	n, err := repo.DeleteExpired(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	clk.Advance(time.Millisecond)

	_, err = repo.Get(ctx, []byte{1})
	require.ErrorIs(t, err, ErrNotFound, "a session is over at its expiry time")

	n, err = repo.DeleteExpired(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "and is swept at that same instant")
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

func TestSecretRowsRewriteCancelledPartWay(t *testing.T) {
	db, _ := openTest(t, Options{})
	repo := db.SecretRows()

	for _, id := range []string{"a", "b"} {
		require.NoError(t, repo.Put(t.Context(), SecretRow{ID: id, KeyID: "k1", Nonce: []byte{1}, Ciphertext: []byte{2}}))
	}

	ctx, cancel := context.WithCancel(t.Context())

	err := repo.Rewrite(ctx, func(r *SecretRow) error {
		r.KeyID = "k2"

		cancel()

		return nil
	})
	require.ErrorIs(t, err, context.Canceled)

	ids, err := repo.KeyIDs(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"k1"}, ids, "a cancelled rewrite saves nothing")
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

func TestEchoDevicesDeclare(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.EchoDevices()

	declared := clk.Now()

	d, err := repo.Declare(ctx, "amzn1.ask.device.D")
	require.NoError(t, err)
	assert.NotZero(t, d.ID)
	assert.Zero(t, d.RequestCount, "declaring is not a request")
	assert.Equal(t, declared, d.FirstSeen)

	d.DisplayName = "Kitchen Echo"
	require.NoError(t, repo.Update(ctx, &d))

	clk.Advance(time.Hour)

	again, err := repo.Declare(ctx, "amzn1.ask.device.D")
	require.NoError(t, err)
	assert.Equal(t, d.ID, again.ID, "declaring twice returns the same device")
	assert.Equal(t, "Kitchen Echo", again.DisplayName, "and changes nothing")
	assert.Equal(t, declared, again.LastSeen)
	assert.Zero(t, again.RequestCount)

	seen, err := repo.Seen(ctx, "amzn1.ask.device.D")
	require.NoError(t, err)
	assert.EqualValues(t, 1, seen.RequestCount)
	assert.Equal(t, clk.Now(), seen.LastSeen)

	_, err = repo.Declare(ctx, "")
	require.ErrorIs(t, err, ErrInvalid)

	_, err = repo.Get(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)

	_, err = repo.GetByAlexaID(ctx, "amzn1.ask.device.unknown")
	require.ErrorIs(t, err, ErrNotFound)

	require.ErrorIs(t, repo.Update(ctx, &EchoDevice{ID: 999}), ErrNotFound)
}

func TestKodiInstancesFailedSetDefaultKeepsThePreviousOne(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.KodiInstances()

	a := newKodi("a")
	a.IsDefaultForUnlinked = true
	require.NoError(t, repo.Create(ctx, a))

	require.ErrorIs(t, repo.SetDefault(ctx, 999), ErrNotFound)

	def, err := repo.Default(ctx)
	require.NoError(t, err, "the default is only cleared when the new one is set")
	assert.Equal(t, a.ID, def.ID)

	b := newKodi("b")
	require.NoError(t, repo.Create(ctx, b))

	b.IsDefaultForUnlinked = true
	require.NoError(t, repo.Update(ctx, b))

	def, err = repo.Default(ctx)
	require.NoError(t, err)
	assert.Equal(t, b.ID, def.ID, "updating an instance to be the default replaces the old one")

	missing := newKodi("missing")
	missing.ID, missing.IsDefaultForUnlinked = 999, true
	require.ErrorIs(t, repo.Update(ctx, missing), ErrNotFound)

	def, err = repo.Default(ctx)
	require.NoError(t, err)
	assert.Equal(t, b.ID, def.ID, "a failed update rolls back clearing the default")
}

func TestRoomsGetAndUpdate(t *testing.T) {
	db, clk := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Rooms()

	room := &Room{Name: "Lounge"}
	require.NoError(t, repo.Create(ctx, room))

	got, err := repo.Get(ctx, room.ID)
	require.NoError(t, err)
	assert.Equal(t, *room, got)

	_, err = repo.Get(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)

	clk.Advance(time.Minute)

	room.Name = "LOUNGE"
	require.NoError(t, repo.Update(ctx, room), "a room may keep its own name")

	room.Name = "Family Room"
	require.NoError(t, repo.Update(ctx, room))
	assert.Equal(t, clk.Now(), room.UpdatedAt)

	got, _, err = repo.Resolve(ctx, "family room")
	require.NoError(t, err)
	assert.Equal(t, room.ID, got.ID)

	_, _, err = repo.Resolve(ctx, "lounge")
	require.ErrorIs(t, err, ErrNotFound, "the old name is released")

	room.Name = "?!"
	require.ErrorIs(t, repo.Update(ctx, room), ErrInvalid)

	room.Name, room.KodiInstanceID = "Family Room", 999
	require.ErrorIs(t, repo.Update(ctx, room), ErrInvalid, "the Kodi instance must exist")

	require.ErrorIs(t, repo.Create(ctx, &Room{Name: "Garage", KodiInstanceID: 999}), ErrInvalid)
	require.ErrorIs(t, repo.Update(ctx, &Room{ID: 999, Name: "Garage"}), ErrNotFound)
	require.ErrorIs(t, repo.Delete(ctx, 999), ErrNotFound)

	_, err = repo.AddAlias(ctx, 999, "attic", SpokenRoom)
	require.ErrorIs(t, err, ErrInvalid, "an alias needs an existing room")

	_, err = repo.AddAlias(ctx, room.ID, "...", SpokenRoom)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestRequestLogInsertKeepsGivenTime(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	melbourne, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	at := time.Date(2026, 10, 26, 21, 30, 0, 123456789, melbourne)
	e := &RequestLogEntry{Time: at}
	require.NoError(t, db.RequestLog().Insert(ctx, e))
	assert.Equal(t, at.UTC().Truncate(time.Millisecond), e.Time)

	got, err := db.RequestLog().Get(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, e.Time, got.Time)
}

func TestRequestLogFailedInsertIsNotPublished(t *testing.T) {
	db, _ := openTest(t, Options{})

	calls := 0
	db.RequestLog().Subscribe(func(RequestLogEntry) { calls++ })

	require.Error(t, db.RequestLog().Insert(t.Context(), &RequestLogEntry{Match: json.RawMessage(`not json`)}))
	assert.Zero(t, calls, "subscribers only see committed entries")
}

func TestSettingsEncodingErrors(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	repo := db.Settings()

	require.ErrorContains(t, repo.Set(ctx, "bad", make(chan int)), "encoding setting")

	_, err := repo.Raw(ctx, "bad")
	require.ErrorIs(t, err, ErrNotFound, "nothing is stored")

	require.NoError(t, repo.Set(ctx, "n", "not a number"))

	v, err := Setting(ctx, repo, "n", 42)
	require.Error(t, err)
	assert.Equal(t, 42, v, "the default is returned with the error")
}

func TestLibraryGroupsSaveIsAtomic(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()
	groups := db.LibraryGroups()

	_, err := groups.Get(ctx, 999)
	require.ErrorIs(t, err, ErrNotFound)

	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{ID: 999}, nil), ErrNotFound)

	g := &LibraryGroup{Name: "Shared"}
	require.ErrorIs(t, groups.Save(ctx, g, []int64{999}), ErrNotFound, "every member must exist")

	list, err := groups.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "a failed save creates no group")

	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{State: "bogus"}, nil), ErrInvalid)
	require.ErrorIs(t, groups.Save(ctx, &LibraryGroup{Evidence: json.RawMessage(`{`)}, nil), ErrInvalid)
}
