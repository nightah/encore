package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func TestInstanceSearchMode(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var created Instance

	c.json(http.MethodPost, "/api/v1/kodi/instances", map[string]string{"name": "Bedroom", "url": h.kodi.URL()}, http.StatusCreated, &created)
	assert.Equal(t, "inherit", created.SearchMode, "follows Settings › Features")
	assert.Empty(t, created.SkinProfile)

	path := fmt.Sprintf("/api/v1/kodi/instances/%d", created.ID)

	var tuned Instance

	c.json(http.MethodPatch, path, map[string]string{"search_mode": "skin", "skin_profile": " skin.arctic.fuse.2 "}, http.StatusOK, &tuned)
	assert.Equal(t, "skin", tuned.SearchMode)
	assert.Equal(t, "skin.arctic.fuse.2", tuned.SkinProfile)

	rec, err := h.app.DB().KodiInstances().Get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "skin", rec.SearchMode)
	assert.Equal(t, "skin.arctic.fuse.2", rec.SkinProfile)

	for field, value := range map[string]string{"skin_profile": "skin.estuary", "search_mode": "native"} {
		resp := c.do(http.MethodPatch, path, map[string]string{field: value})
		assert.Equal(t, http.StatusUnprocessableEntity, resp.status, field)
	}

	p := c.do(http.MethodPatch, path, map[string]string{"skin_profile": "skin.unknown"}).problem(t)
	assert.Equal(t, "body.skin_profile", p.Errors[0].Location)
	assert.Contains(t, p.Errors[0].Message, "skin.arctic.fuse.3")

	in := InstanceInput{SearchMode: new("skins")}
	require.Error(t, applyInstanceBehaviour(&rec, in), "checked without the API's own validation too, as an import is")

	c.json(http.MethodPatch, path, map[string]string{"search_mode": "inherit", "skin_profile": ""}, http.StatusOK, &tuned)
	assert.Equal(t, "inherit", tuned.SearchMode)
	assert.Empty(t, tuned.SkinProfile)

	rec, err = h.app.DB().KodiInstances().Get(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Empty(t, rec.SearchMode, "inherit is stored as empty")
}

func TestInstanceSkin(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()

	var list []Instance

	c.json(http.MethodGet, "/api/v1/kodi/instances", nil, http.StatusOK, &list)
	require.Len(t, list, 1)

	path := fmt.Sprintf("/api/v1/kodi/instances/%d/skin", list[0].ID)

	var skin InstanceSkin

	require.Eventually(t, func() bool {
		return c.do(http.MethodGet, path, nil).status == http.StatusOK
	}, 10*time.Second, 50*time.Millisecond, "the instance runs")

	c.json(http.MethodGet, path, nil, http.StatusOK, &skin)
	assert.Equal(t, "global_search", skin.SearchMode)
	require.NotNil(t, skin.Skin.V)
	assert.Equal(t, Skin{ID: "skin.estuary", Name: "Estuary", Profile: "skin.estuary", Search: "native_global_search", Missing: []string{}}, *skin.Skin.V)

	c.json(http.MethodPatch, "/api/v1/settings", map[string]any{"features": map[string]any{"search_mode": "skin"}}, http.StatusOK, nil)
	h.kodi.SetSkin("skin.arctic.fuse.3")

	c.json(http.MethodGet, path, nil, http.StatusOK, &skin)
	assert.Equal(t, "skin", skin.SearchMode, "Settings › Features applies")
	require.NotNil(t, skin.Skin.V)
	assert.Equal(t, Skin{ID: "skin.arctic.fuse.3", Name: "Arctic Fuse 3", Profile: "skin.arctic.fuse.3", Search: "missing_addons", Missing: []string{"script.skinvariables"}}, *skin.Skin.V, "a skin change shows at once")

	h.kodi.Handle("Settings.GetSettingValue", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: kodi.CodeInternalError, Message: "Internal error."}
	})

	c.json(http.MethodGet, path, nil, http.StatusOK, &skin)
	assert.Nil(t, skin.Skin.V, "no skin when Kodi can't say")
	assert.Equal(t, "skin", skin.SearchMode)

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/kodi/instances/999/skin", nil).status)
}

func TestSkinProfilesEndpoint(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	var profiles []kodi.SkinProfile

	c.json(http.MethodGet, "/api/v1/kodi/skin-profiles", nil, http.StatusOK, &profiles)
	assert.Equal(t, kodi.SkinProfiles(), profiles)

	i := slices.IndexFunc(profiles, func(p kodi.SkinProfile) bool { return p.ID == "skin.nimbus" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, kodi.SkinProfile{ID: "skin.nimbus", Name: "Nimbus", Requires: []string{"script.nimbus.helper"}, Window: "1121", How: "keyboard"}, profiles[i])
}
