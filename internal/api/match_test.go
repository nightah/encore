package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchUnknownLibrary(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true})
	c := h.admin()

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/match", map[string]any{"library_id": 999, "query": "hot fuzz"}).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/v1/aliases?library_id=999", nil).status)
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": 999, "kind": "movie", "media_id": 6, "alias": "x"}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, "/api/v1/aliases", nil).status, "library_id is required")
	assert.Equal(t, http.StatusNotFound, c.do(http.MethodDelete, "/api/v1/aliases/999", nil).status)
}

func TestMatchValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lib := h.libraryID(t)

	for name, body := range map[string]map[string]any{
		"empty query":    {"library_id": lib, "query": ""},
		"query too long": {"library_id": lib, "query": strings.Repeat("a", 501)},
		"unknown kind":   {"library_id": lib, "query": "dune", "kinds": []string{"film"}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/match", body).status, name)
	}

	var res MatchResult

	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "dune", "kinds": []string{"tvshow"}}, http.StatusOK, &res)

	for _, cand := range res.Candidates {
		assert.Equal(t, "tvshow", cand.Kind, "only the kinds asked for")
	}

	c.json(http.MethodPost, "/api/v1/match", map[string]any{"library_id": lib, "query": "zzzz qqqq xxxx"}, http.StatusOK, &res)
	assert.Equal(t, "not_found", res.Decision)
	assert.NotNil(t, res.Candidates, "an empty list, not null")
	assert.LessOrEqual(t, len(res.Candidates), 10)
}

func TestAliasValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.admin()
	lib := h.libraryID(t)

	p := c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "movie", "media_id": 6, "alias": "!!!"}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	require.Len(t, p.Errors, 1)
	assert.Equal(t, "body.alias", p.Errors[0].Location, "an alias nobody could say is refused")

	p = c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "tvshow", "media_id": 6, "alias": "fuzz"}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status, "the kind is part of the title's identity")
	assert.Equal(t, "body.media_id", p.Errors[0].Location)

	for name, body := range map[string]map[string]any{
		"no media":       {"library_id": lib, "kind": "movie", "media_id": 0, "alias": "x"},
		"unknown kind":   {"library_id": lib, "kind": "film", "media_id": 6, "alias": "x"},
		"alias too long": {"library_id": lib, "kind": "movie", "media_id": 6, "alias": strings.Repeat("a", 141)},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodPost, "/api/v1/aliases", body).status, name)
	}

	var alias TitleAlias

	c.json(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": lib, "kind": "movie", "media_id": 5, "alias": "  le fabuleux destin  "}, http.StatusCreated, &alias)
	assert.Equal(t, "le fabuleux destin", alias.Alias, "aliases are trimmed")
	assert.Equal(t, "Amélie", alias.Title)
	assert.Equal(t, "movie", alias.Kind)
	assert.Equal(t, 5, alias.MediaID)

	var aliases []TitleAlias

	c.json(http.MethodGet, fmt.Sprintf("/api/v1/aliases?library_id=%d", lib), nil, http.StatusOK, &aliases)
	assert.Equal(t, []TitleAlias{alias}, aliases)
}

func TestMatchWithoutALoadedLibrary(t *testing.T) {
	h := newHarness(t, harnessOpts{noKodi: true, mutate: func(o *Options) {
		o.Libraries = &fakeLibraries{lib: Library{ID: 1, Name: "Garage", SourceInstanceID: 999}}
	}})
	c := h.admin()

	assert.Equal(t, http.StatusNotFound, c.do(http.MethodPost, "/api/v1/match", map[string]any{"library_id": 1, "query": "hot fuzz"}).status, "a library whose instance isn't loaded can't be searched")

	p := c.do(http.MethodPost, "/api/v1/aliases", map[string]any{"library_id": 1, "kind": "movie", "media_id": 6, "alias": "the cornetto one"}).problem(t)
	assert.Equal(t, http.StatusUnprocessableEntity, p.Status)
	assert.Equal(t, "body.media_id", p.Errors[0].Location)

	resp := c.do(http.MethodGet, "/api/v1/aliases?library_id=1", nil)
	require.Equal(t, http.StatusOK, resp.status)
	assert.JSONEq(t, `[]`, string(resp.body))
}
