package main

import (
	"strings"
	"testing"
)

func testSpec(t *testing.T, types, methods string) *Spec {
	t.Helper()
	spec := &Spec{Version: APIVersion{13, 1000, 0}, Notifications: map[string]*Schema{}}
	var err error
	if spec.Types, err = decodeSchemaMap(types); err != nil {
		t.Fatalf("types: %v", err)
	}
	if spec.Methods, err = decodeSchemaMap(methods); err != nil {
		t.Fatalf("methods: %v", err)
	}
	return spec
}

func decodeSchemaMap(src string) (map[string]*Schema, error) {
	var props Props
	if err := props.UnmarshalJSON(stripComments([]byte(src))); err != nil {
		return nil, err
	}
	m := map[string]*Schema{}
	for _, p := range props {
		m[p.Name] = p.Schema
	}
	return m, nil
}

// generateAll collapses whitespace so assertions are independent of gofmt alignment.
func generateAll(t *testing.T, spec, compat *Spec) string {
	t.Helper()
	files, err := generate(spec, compat, "kodirpc")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, name := range sortedKeys(files) {
		b.Write(files[name])
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func assertContains(t *testing.T, src string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(src, want) {
			t.Errorf("generated code lacks %q", want)
		}
	}
}

func TestExtendsEmbedsSingleObjectParent(t *testing.T) {
	src := generateAll(t, testSpec(t, `{
		"Item.Details.Base": {"type": "object", "properties": {"label": {"type": "string", "required": true}}},
		"Media.Details.Base": {"extends": "Item.Details.Base", "properties": {"fanart": {"type": "string"}}}
	}`, `{}`), nil)

	assertContains(t, src,
		"type MediaDetailsBase struct { ItemDetailsBase Fanart string `json:\"fanart,omitempty\"` }",
		"type ItemDetailsBase struct { Label string `json:\"label\"` }",
	)
}

func TestExtendsFlattensMultipleParents(t *testing.T) {
	src := generateAll(t, testSpec(t, `{
		"Base": {"type": "object", "properties": {"label": {"type": "string", "required": true}}},
		"Video": {"extends": "Base", "properties": {"runtime": {"type": "integer"}}},
		"Audio": {"extends": "Base", "properties": {"label": {"type": "string"}, "bpm": {"type": "integer"}}},
		"Item": {"extends": ["Video", "Audio"], "properties": {"type": {"type": "string"}}}
	}`, `{}`), nil)

	// The diamond on "label" must collapse to one field; later parents win.
	assertContains(t, src,
		"type Item struct { Label string `json:\"label,omitempty\"` Runtime int `json:\"runtime,omitempty\"` "+
			"Bpm int `json:\"bpm,omitempty\"` Type string `json:\"type,omitempty\"` }",
	)
}

func TestExtendsMergesNonObjectSchemas(t *testing.T) {
	src := generateAll(t, testSpec(t, `{
		"Item.Fields.Base": {"type": "array", "uniqueItems": true, "items": {"type": "string"}},
		"Video.Fields.Movie": {"extends": "Item.Fields.Base", "items": {"type": "string", "enum": ["title", "year"]}}
	}`, `{}`), nil)

	assertContains(t, src,
		"type VideoFieldsMovie []VideoFieldsMovieItem",
		"type VideoFieldsMovieItem string",
		`VideoFieldsMovieItemTitle VideoFieldsMovieItem = "title"`,
		`VideoFieldsMovieItemYear VideoFieldsMovieItem = "year"`,
		"type ItemFieldsBase []string",
	)
}

func TestUnions(t *testing.T) {
	src := generateAll(t, testSpec(t, `{
		"Library.Id": {"type": "integer", "default": -1, "minimum": 1},
		"Global.Toggle": {"type": [{"type": "boolean", "required": true}, {"type": "string", "enum": ["toggle"], "required": true}]},
		"Optional.String": {"type": ["null", "string"], "default": null},
		"Item": {"type": ["null", {"$ref": "Library.Id"}, {"type": "object", "properties": {"path": {"type": "string", "required": true}}}]}
	}`, `{}`), nil)

	assertContains(t, src,
		"type GlobalToggle struct{ union }",
		"func NewGlobalToggleFromBool(v bool) GlobalToggle { return GlobalToggle{newUnion(v)} }",
		"func (u GlobalToggle) AsString() (GlobalToggleString, error)",
		`GlobalToggleStringToggle GlobalToggleString = "toggle"`,
		// A union of null and one alternative is a pointer.
		"type OptionalString = *string",
		// Inline object alternatives are named after their required properties.
		"func NewItemFromLibraryID(v LibraryID) Item",
		"func NewItemFromPath(v ItemPath) Item",
		"type ItemPath struct { Path string `json:\"path\"` }",
		"It holds one of: LibraryID, ItemPath, null.",
	)
}

func TestFieldOptionality(t *testing.T) {
	src := generateAll(t, testSpec(t, `{
		"Sub": {"type": "object", "properties": {"x": {"type": "integer"}}},
		"Mode": {"type": "string", "enum": ["a", "b"]},
		"Toggle": {"type": ["boolean", "string"]}
	}`, `{
		"Ns.Do": {"type": "method", "description": "Does it", "permission": "ReadData", "transport": "Response",
			"params": [
				{"name": "id", "type": "integer", "required": true},
				{"name": "sub", "$ref": "Sub"},
				{"name": "reqsub", "$ref": "Sub", "required": true},
				{"name": "flag", "type": "boolean", "default": false},
				{"name": "descriptions", "type": "boolean", "default": true},
				{"name": "count", "type": "integer", "default": 5000},
				{"name": "mode", "$ref": "Mode", "default": "a"},
				{"name": "toggle", "$ref": "Toggle"},
				{"name": "tags", "type": "array", "items": {"type": "string"}}
			],
			"returns": {"$ref": "Sub"}}
	}`), nil)

	assertContains(t, src,
		"ID int `json:\"id\"`",
		"Sub *Sub `json:\"sub,omitempty\"`",
		"Reqsub Sub `json:\"reqsub\"`",
		"Flag bool `json:\"flag,omitempty\"`",
		// A non-zero default must stay expressible, so zero is sent via a pointer.
		"Descriptions *bool `json:\"descriptions,omitempty\"`",
		"Count *int `json:\"count,omitempty\"`",
		// The empty string is not a legal enum value, so omitempty suffices.
		"Mode Mode `json:\"mode,omitempty\"`",
		"Toggle Toggle `json:\"toggle,omitzero\"`",
		"Tags []string `json:\"tags,omitempty\"`",
		"type NsDoResult = Sub",
		"func (c Client) NsDo(ctx context.Context, p NsDoParams) (NsDoResult, error)",
		"return Call[NsDoParams, NsDoResult](ctx, c.Caller, MethodNsDo, p)",
		`MethodNsDo = "Ns.Do"`,
	)
}

func TestAvailability(t *testing.T) {
	methods := `{
		"A.Old": {"type": "method", "params": [{"name": "x", "type": "integer"}], "returns": "string"},
		"A.New": {"type": "method", "params": [], "returns": "string"}
	}`
	spec := testSpec(t, `{}`, methods)
	compat := testSpec(t, `{}`, `{"A.Old": {"type": "method", "params": [], "returns": "string"}}`)
	compat.Version = APIVersion{13, 5, 0}

	src := generateAll(t, spec, compat)
	assertContains(t, src,
		`MethodANew: { Name: "A.New", Since: APIVersion{13, 1000, 0}, },`,
		`MethodAOld: { Name: "A.Old", Params: []string{"x"}, Since: APIVersion{13, 5, 0}, ParamsSince: map[string]APIVersion{ "x": {13, 1000, 0}, }, },`,
		"var BaselineVersion = APIVersion{13, 5, 0}",
		// Methods without parameters get a wrapper without a params argument.
		"func (c Client) ANew(ctx context.Context) (ANewResult, error)",
	)
}

func TestNameCollisionIsReported(t *testing.T) {
	spec := testSpec(t, `{
		"A.B": {"type": "string"},
		"AB": {"type": "string"}
	}`, `{}`)
	if _, err := generate(spec, nil, "kodirpc"); err == nil {
		t.Fatal("expected an identifier collision error")
	}
}

func TestUnknownReferenceIsReported(t *testing.T) {
	spec := testSpec(t, `{"A": {"$ref": "Missing"}}`, `{}`)
	if _, err := generate(spec, nil, "kodirpc"); err == nil {
		t.Fatal("expected an unknown reference error")
	}
}
