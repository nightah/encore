package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoomKodiInstance(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lounge := h.loungeID()

	var room Room

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "  Lounge  ", "kodi_instance_id": lounge}, http.StatusCreated, &room)
	assert.Equal(t, "Lounge", room.Name, "names are trimmed")
	require.NotNil(t, room.KodiInstanceID)
	assert.Equal(t, lounge, *room.KodiInstanceID)

	path := fmt.Sprintf("/api/v1/rooms/%d", room.ID)

	c.json(http.MethodPatch, path, map[string]any{"name": "Living Room"}, http.StatusOK, &room)
	assert.Equal(t, "Living Room", room.Name)
	require.NotNil(t, room.KodiInstanceID, "absent fields are left alone")

	c.json(http.MethodPatch, path, map[string]any{"kodi_instance_id": nil}, http.StatusOK, &room)
	assert.Nil(t, room.KodiInstanceID, "null unlinks")
	assert.Equal(t, "Living Room", room.Name)

	c.json(http.MethodPatch, path, map[string]any{"kodi_instance_id": lounge}, http.StatusOK, &room)
	require.NotNil(t, room.KodiInstanceID)

	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, path, map[string]any{"kodi_instance_id": 999}).status, "an unknown Kodi instance is refused")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Den", "kodi_instance_id": 999}).status)

	var rooms []Room

	c.json(http.MethodGet, "/api/v1/rooms", nil, http.StatusOK, &rooms)
	require.Len(t, rooms, 1, "the refused room was not created")
	assert.Equal(t, lounge, *rooms[0].KodiInstanceID)
}

func TestRoomValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	resp := c.do(http.MethodGet, "/api/v1/rooms", nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.JSONEq(t, `[]`, string(resp.body), "an empty list, not null")

	for name, body := range map[string]map[string]any{
		"no name":         {},
		"empty name":      {"name": ""},
		"name too long":   {"name": strings.Repeat("x", 65)},
		"name not text":   {"name": 7},
		"blank name":      {"name": "   "},
		"punctuation":     {"name": "!!"},
		"instance not id": {"name": "Den", "kodi_instance_id": "lounge"},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/rooms", body).status, name)
	}

	var room Room

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Küche"}, http.StatusCreated, &room)
	assert.Equal(t, "Küche", room.Name)

	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "KÜCHE"}).status, "names are unique whatever their case")
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPatch, "/api/v1/rooms/999", map[string]any{"name": "Den"}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPatch, fmt.Sprintf("/api/v1/rooms/%d", room.ID), map[string]any{"name": ""}).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, "/api/v1/rooms/999", nil).status)
}

func TestRoomAliasValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var room Room

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Lounge"}, http.StatusCreated, &room)

	aliases := fmt.Sprintf("/api/v1/rooms/%d/aliases", room.ID)

	var alias RoomAlias

	c.json(http.MethodPost, aliases, map[string]any{"alias": "  front room  "}, http.StatusCreated, &alias)
	assert.Equal(t, "front room", alias.Alias, "aliases are trimmed")

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/rooms/999/aliases", map[string]any{"alias": "den"}).status, "a room must exist")
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, aliases, map[string]any{"alias": ""}).status)
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, aliases, map[string]any{"alias": "lounge"}).status, "an alias may not repeat a room's name")
	assert.Equal(t, http.StatusConflict, c.do(http.MethodPost, aliases, map[string]any{"alias": "Front Room"}).status, "nor another alias")

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, fmt.Sprintf("/api/v1/rooms/999/aliases/%d", alias.ID), nil).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, fmt.Sprintf("%s/999", aliases), nil).status)

	c.json(http.MethodDelete, fmt.Sprintf("/api/v1/rooms/%d", room.ID), nil, http.StatusNoContent, nil)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, fmt.Sprintf("%s/%d", aliases, alias.ID), nil).status, "a removed room takes its aliases with it")

	c.json(http.MethodPost, "/api/v1/rooms", map[string]any{"name": "Front Room"}, http.StatusCreated, nil)
}
