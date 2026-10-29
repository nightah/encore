package docsgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiSpec exercises every shape the endpoints page renders: public and protected routes, query defaults, request bodies with optional keys, nullable and nested objects, enums, maps, lists of plain values, media types other than JSON, a self-referencing schema, and a resource two sections return.
const apiSpec = `{
  "tags": [
    {"name": "things", "description": "Things and their parts.", "x-displayName": "Things"},
    {"name": "empty", "description": "No operations."},
    {"name": "other"}
  ],
  "paths": {
    "/api/v1/things": {
      "get": {"summary": "List things", "tags": ["things"], "security": [{"session": []}],
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer", "default": 50, "minimum": 1, "maximum": 200}},
          {"name": "q", "in": "query", "schema": {"type": "string", "description": "Search the names", "minimum": 0, "maximum": 9}},
          {"name": "raw", "in": "query"},
          {"name": "plain", "in": "query", "schema": {"type": "string"}},
          {"name": "id", "in": "path", "schema": {"type": "integer"}}
        ],
        "responses": {
          "200": {"content": {"application/json": {"schema": {"type": "array", "items": {"$ref": "#/components/schemas/Thing"}}}}},
          "default": {"content": {"application/problem+json": {"schema": {"$ref": "#/components/schemas/Problem"}}}}
        }},
      "post": {"summary": "Add a thing", "description": "409 if the name is taken", "tags": ["things"], "security": [{"session": []}],
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/ThingInput"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}}}}}
    },
    "/api/v1/things/{id}": {
      "delete": {"summary": "Remove a thing.", "tags": ["things"], "security": [{"session": []}], "responses": {"204": {}}}
    },
    "/api/v1/things/{id}/export": {
      "get": {"summary": "Export a thing", "tags": ["things"], "security": [{"session": []}],
        "responses": {"200": {"content": {"application/yaml": {"schema": {"type": "string"}}}}}},
      "post": {"summary": "Import a thing", "tags": ["things"], "security": [{"session": []}],
        "requestBody": {"content": {"application/yaml": {"schema": {"type": "string"}}}},
        "responses": {"202": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Accepted"}}}}}}
    },
    "/api/v1/callback": {
      "get": {"summary": "Finish signing in", "tags": ["other"], "responses": {"302": {}}}
    },
    "/api/v1/other": {
      "get": {"summary": "Things again", "tags": ["other"],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}}}}}
    }
  },
  "components": {"schemas": {
    "Thing": {"type": "object", "required": ["id", "name"], "properties": {
      "id": {"type": "integer"},
      "name": {"type": "string", "description": "What it's called"},
      "state": {"type": "string", "enum": ["on", "off", ""]},
      "labels": {"type": "array", "items": {"type": "string"}},
      "loose": {"type": "array"},
      "meta": {"type": "object", "additionalProperties": {"type": "string"}},
      "by_size": {"type": "object", "additionalProperties": {"$ref": "#/components/schemas/Deeper"}},
      "sealed": {"type": "object", "additionalProperties": false},
      "blob": {"type": "object"},
      "opaque": {"type": "string"},
      "owner": {"oneOf": [{"$ref": "#/components/schemas/Owner"}, {"type": "null"}], "description": "null when nobody owns it"},
      "parts": {"type": "array", "items": {"$ref": "#/components/schemas/Part"}, "description": "In the order they fit"},
      "accepted": {"$ref": "#/components/schemas/Accepted"},
      "unknown": {"$ref": "#/components/schemas/Missing"},
      "either": {"oneOf": [{"type": "string"}, {"type": "integer"}]},
      "nothing": {"oneOf": [{"type": "string"}, {"type": "null"}]},
      "tags": {"type": ["array", "null"], "items": {"type": "string", "description": "A tag"}}
    }},
    "Owner": {"type": "object", "properties": {"name": {"type": "string", "description": "The owner's name"}, "deep": {"$ref": "#/components/schemas/Deep"}}},
    "Deep": {"type": "object", "properties": {"deeper": {"$ref": "#/components/schemas/Deeper"}}},
    "Deeper": {"type": "object", "properties": {"x": {"type": "string"}}},
    "Part": {"type": "object", "properties": {"label": {"type": "string", "description": "Printed on it"}, "of": {"$ref": "#/components/schemas/Part"}}},
    "ThingInput": {"type": "object", "required": ["name"], "properties": {"name": {"type": "string", "description": "Unique among things"}, "state": {"type": "string", "enum": ["on", "off"], "default": "on"}, "owner": {"$ref": "#/components/schemas/Owner"}}},
    "Accepted": {"type": "object", "properties": {"id": {"type": "string"}}},
    "Problem": {"type": "object", "properties": {"detail": {"type": "string"}}}
  }}
}`

func TestAPIEndpoints(t *testing.T) {
	t.Parallel()

	out, err := APIEndpoints([]byte(apiSpec))
	require.NoError(t, err)

	page := string(out)

	assert.Contains(t, page, GeneratedMarker)
	assert.Contains(t, page, "## Things\n\nThings and their parts.\n", "a tag's display name and description head its section")
	assert.NotContains(t, page, "## empty", "a tag without operations has no section")
	assert.Contains(t, page, "## other\n", "a tag without a display name is titled by its name")

	assert.Equal(t, 1, strings.Count(page, "`Thing`:"), "a resource two sections return is shown once")
	assert.Less(t, strings.Index(page, "`Thing`:"), strings.Index(page, "## other"), "in the first section that returns it")
	assert.Contains(t, page, "`Accepted`:")
	assert.NotContains(t, page, "`Problem`:", "error responses aren't resources")

	assert.Contains(t, page, "```text\n{\n  \"id\",\n  \"name\",\n  \"accepted\": Accepted,\n  \"blob\",\n", "required keys first in order, the rest alphabetically, and resources by name")
	assert.Contains(t, page, `"labels": […]`, "a list of plain values")
	assert.Contains(t, page, `"loose": […]`, "a list that doesn't say what it holds")
	assert.Contains(t, page, `"meta": {}`, "a map of plain values")
	assert.Contains(t, page, `"by_size": {"…": {"x"}}`, "a map of objects")
	assert.Contains(t, page, `"sealed": {}`)
	assert.Contains(t, page, `  "blob",`, "an object that says nothing about its keys")
	assert.Contains(t, page, `"nothing",`, "a nullable plain value")
	assert.Contains(t, page, `"owner": {"deep": {"deeper": {…}}, "name"} | null`, "a nullable object, nested two levels below the key")
	assert.Contains(t, page, `"parts": [{"label", "of": {"label", "of": {…}}}]`)
	assert.Contains(t, page, `"state": "on|off"`, "an enum without its empty value")
	assert.Contains(t, page, `"tags": […]`)
	assert.Contains(t, page, `"unknown": Missing`, "a reference to a schema the document lacks")

	assert.Contains(t, page, "- `name`: What it's called.\n", "field notes end with a full stop")
	assert.Contains(t, page, "- `owner`: null when nobody owns it.\n- `owner.name`: The owner's name.\n", "fields inside nested objects")
	assert.Contains(t, page, "- `parts`: In the order they fit.\n- `parts[].label`: Printed on it.\n", "fields inside lists, without following a schema back into itself")
	assert.Contains(t, page, "- `tags`: A tag.\n", "a list's note comes from its items when it has none")
	assert.NotContains(t, page, "`accepted.id`", "a resource's fields are listed under its own block")

	assert.Contains(t, page, "| GET | `/things?limit=50&q=&raw=&plain=` | List things.<br />`[Thing]`<br />`limit`: From 1 to 200.<br />`q`: Search the names (0 to 9). |", "query parameters with defaults, their notes and ranges, and a list of a resource")
	assert.Contains(t, page, "| POST | `/things` | Add a thing. 409 if the name is taken.<br />"+
		"`{\"name\", \"owner\"?: {…}, \"state\"?: \"on\\|off\"}` → `201 Thing`<br />`name`: Unique among things.<br />`state`: Defaults to `on`. |", "a request body names the keys it may leave out")
	assert.Contains(t, page, "| DELETE | `/things/{id}` | Remove a thing.<br />`204` |")
	assert.Contains(t, page, "| GET | `/things/{id}/export` | Export a thing.<br />`application/yaml` |")
	assert.Contains(t, page, "| POST | `/things/{id}/export` | Import a thing.<br />`application/yaml` → `202 Accepted` |")
	assert.Contains(t, page, "| GET | `/callback` | *Public.* Finish signing in. |", "a public route, and one with nothing to show")
	assert.Contains(t, page, "| GET | `/other` | *Public.* Things again.<br />`Thing` |")
}

func TestAPIEndpointsRefusesWhatItCantPlace(t *testing.T) {
	t.Parallel()

	_, err := APIEndpoints([]byte("{"))
	require.Error(t, err)

	_, err = APIEndpoints([]byte(`{"tags": [{"name": "a"}], "paths": {"/api/v1/x": {"get": {"summary": "X", "tags": ["b"]}}}}`))
	require.ErrorContains(t, err, "GET /api/v1/x has no tag declared in the document")
}

func TestGenerateNeedsTheOpenAPIDocument(t *testing.T) {
	t.Parallel()

	_, err := Generate(t.TempDir())
	require.Error(t, err)

	// Without the document, and with one the endpoints page can't be rendered from, the whole run stops.
	root := t.TempDir()
	require.NoError(t, copyTree(filepath.Join(root, "internal", "config"), filepath.Join("..", "config")))

	_, err = Generate(root)
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "api"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "api", "openapi.json"), []byte("{"), 0o644))

	_, err = Generate(root)
	require.ErrorContains(t, err, "API endpoints")
}

func copyTree(dst, src string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
}
