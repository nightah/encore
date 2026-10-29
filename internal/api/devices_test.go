package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
)

func TestOptionalID(t *testing.T) {
	t.Parallel()

	type patch struct {
		RoomID OptionalID `json:"room_id"`
	}

	tests := []struct {
		body string
		want OptionalID
	}{
		{`{}`, OptionalID{}},
		{`{"room_id":null}`, OptionalID{Set: true}},
		{`{"room_id":7}`, OptionalID{Set: true, ID: 7}},
		{`{"room_id":0}`, OptionalID{Set: true}},
		{`{"room_id":9007199254740993}`, OptionalID{Set: true, ID: 9007199254740993}},
	}

	for _, tt := range tests {
		var p patch
		require.NoError(t, json.Unmarshal([]byte(tt.body), &p), tt.body)
		assert.Equal(t, tt.want, p.RoomID, "absent, null and a value are told apart: %s", tt.body)
	}

	for _, body := range []string{`{"room_id":"7"}`, `{"room_id":1.5}`, `{"room_id":{}}`} {
		var p patch
		require.Error(t, json.Unmarshal([]byte(body), &p), body)
	}

	s := OptionalID{}.Schema(huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer))
	assert.Equal(t, huma.TypeInteger, s.Type)
	assert.Equal(t, "int64", s.Format)
	assert.True(t, s.Nullable)
}

func TestIDPtr(t *testing.T) {
	t.Parallel()

	assert.Nil(t, idPtr(0), "zero means unset")

	p := idPtr(42)
	require.NotNil(t, p)
	assert.Equal(t, int64(42), *p)
}

func TestUpdateDeviceValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	dev, err := h.app.DB().EchoDevices().Seen(t.Context(), "amzn1.ask.device.DEN")
	require.NoError(t, err)

	path := fmt.Sprintf("/api/v1/devices/%d", dev.ID)

	var got Device

	c.json(http.MethodPatch, path, map[string]any{"name": "  Den Echo  "}, http.StatusOK, &got)
	assert.Equal(t, "Den Echo", got.Name, "names are trimmed")
	assert.Equal(t, "amzn1.ask.device.DEN", got.AlexaDeviceID)

	c.json(http.MethodPatch, path, map[string]any{"name": "Küche Echo 🎵"}, http.StatusOK, &got)
	assert.Equal(t, "Küche Echo 🎵", got.Name)

	c.json(http.MethodPatch, path, map[string]any{}, http.StatusOK, &got)
	assert.Equal(t, "Küche Echo 🎵", got.Name, "an empty patch changes nothing")

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, path, map[string]any{"name": strings.Repeat("x", 65)}).status, "names are at most 64 characters")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, path, map[string]any{"kodi_instance_id": 99}).status, "an unknown Kodi instance is refused")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, path, map[string]any{"room_id": "kitchen"}).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPatch, "/api/v1/devices/999", map[string]any{"name": "Ghost"}).status)

	var list []Device

	c.json(http.MethodGet, "/api/v1/devices", nil, http.StatusOK, &list)
	require.Len(t, list, 1)
	assert.Equal(t, "Küche Echo 🎵", list[0].Name, "refused patches changed nothing")
	assert.Nil(t, list[0].RoomID)
}

func TestDeviceChangesAreStreamed(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	dev, err := h.app.DB().EchoDevices().Seen(t.Context(), "amzn1.ask.device.DEN")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, resp := c.stream(ctx, "")
	defer resp.Body.Close()

	next(t, ch, "comment")

	c.json(http.MethodPatch, fmt.Sprintf("/api/v1/devices/%d", dev.ID), map[string]any{"name": "Den Echo"}, http.StatusOK, nil)

	var d Device

	require.NoError(t, json.Unmarshal([]byte(next(t, ch, "device").data), &d))
	assert.Equal(t, dev.ID, d.ID)
	assert.Equal(t, "Den Echo", d.Name, "other open pages see the new name")
}

func TestDevicesWithNone(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	resp := c.do(http.MethodGet, "/api/v1/devices", nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.JSONEq(t, `[]`, string(resp.body), "an empty list, not null")
}

func TestRequestsTodayFollowTheConfiguredTimeZone(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()
	ctx := t.Context()

	mel, err := time.LoadLocation("Australia/Melbourne")
	require.NoError(t, err)

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"locale": map[string]any{"timezone": "Australia/Melbourne"}}, http.StatusOK, nil)

	// Breakfast in Melbourne is still the previous evening in UTC.
	breakfast := time.Date(2026, 10, 28, 8, 0, 0, 0, mel)
	h.skew.Store(int64(time.Until(breakfast)))

	kitchen, err := h.app.DB().EchoDevices().Seen(ctx, "amzn1.ask.device.KITCHEN")
	require.NoError(t, err)

	_, err = h.app.DB().EchoDevices().Seen(ctx, "amzn1.ask.device.DEN")
	require.NoError(t, err)

	log := h.app.DB().RequestLog()
	for _, e := range []store.RequestLogEntry{
		{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchMovie", Time: time.Date(2026, 10, 27, 23, 59, 0, 0, mel)},
		{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchMovie", Time: time.Date(2026, 10, 28, 0, 1, 0, 0, mel)},
		{DeviceID: "amzn1.ask.device.KITCHEN", Intent: "WatchShow", Time: time.Date(2026, 10, 28, 7, 0, 0, 0, mel)},
		{DeviceID: "amzn1.ask.device.DEN", Intent: "WatchShow", Time: time.Date(2026, 10, 28, 7, 30, 0, 0, mel)},
	} {
		require.NoError(t, log.Insert(ctx, &e))
	}

	var list []Device

	c.json(http.MethodGet, "/api/v1/devices", nil, http.StatusOK, &list)

	today := map[string]int{}
	for _, d := range list {
		today[d.AlexaDeviceID] = d.RequestCountToday
	}

	assert.Equal(t, map[string]int{"amzn1.ask.device.KITCHEN": 2, "amzn1.ask.device.DEN": 1}, today, "a request a minute before local midnight was yesterday")

	var one Device

	c.json(http.MethodPatch, fmt.Sprintf("/api/v1/devices/%d", kitchen.ID), map[string]any{"name": "Kitchen"}, http.StatusOK, &one)
	assert.Equal(t, 2, one.RequestCountToday, "one device counts only its own requests")

	// The next morning nothing has been asked yet, so even the newest request was yesterday.
	h.skew.Store(int64(time.Until(breakfast.AddDate(0, 0, 1))))

	c.json(http.MethodGet, "/api/v1/devices", nil, http.StatusOK, &list)

	require.Len(t, list, 2)

	for _, d := range list {
		assert.Zero(t, d.RequestCountToday, d.AlexaDeviceID)
	}

	c.json(http.MethodPatch, fmt.Sprintf("/api/v1/devices/%d", kitchen.ID), map[string]any{}, http.StatusOK, &one)
	assert.Zero(t, one.RequestCountToday)
}
