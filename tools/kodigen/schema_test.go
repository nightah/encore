package main

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestStripComments(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"line comment", "{\"a\": 1} // trailing\n", "{\"a\": 1}            \n"},
		{"full line", "// head\n{}", "       \n{}"},
		{"block", "{/* x */\"a\":1}", "{       \"a\":1}"},
		{"multi-line block keeps newlines", "/*a\nb*/{}", "   \n   {}"},
		{"slashes in string", `{"u": "http://kodi.wiki"}`, `{"u": "http://kodi.wiki"}`},
		{"escaped quote in string", `{"u": "a\"//b"} //c`, `{"u": "a\"//b"}    `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(stripComments([]byte(tt.in))); got != tt.want {
				t.Errorf("stripComments(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func decodeSchema(t *testing.T, src string) *Schema {
	t.Helper()
	s := new(Schema)
	if err := json.Unmarshal(stripComments([]byte(src)), s); err != nil {
		t.Fatalf("decode %s: %v", src, err)
	}
	return s
}

func TestSchemaDecodeKeywords(t *testing.T) {
	s := decodeSchema(t, `{
		// a comment
		"type": ["null", "string", {"$ref": "Library.Id"}],
		"extends": "Item.Details.Base",
		"required": "true",
		"transport": ["Response", "FileDownloadDirect"],
		"additionalProperties": false,
		"properties": {"zeta": {"type": "integer"}, "alpha": {"type": "string", "required": false}}
	}`)

	if s.Type != "" || len(s.Union) != 3 {
		t.Fatalf("union not decoded: type=%q union=%d", s.Type, len(s.Union))
	}
	if s.Union[0].Type != "null" || s.Union[1].Type != "string" || s.Union[2].Ref != "Library.Id" {
		t.Errorf("union alternatives = %+v %+v %+v", s.Union[0], s.Union[1], s.Union[2])
	}
	if !slices.Equal(s.Extends, []string{"Item.Details.Base"}) {
		t.Errorf("extends = %v", s.Extends)
	}
	if !s.Required {
		t.Error(`"required": "true" not honoured`)
	}
	if !slices.Equal(s.Transport, []string{"Response", "FileDownloadDirect"}) {
		t.Errorf("transport = %v", s.Transport)
	}
	if s.AdditionalProperties != nil {
		t.Error("additionalProperties: false should decode to nil")
	}
	if len(s.Properties) != 2 || s.Properties[0].Name != "zeta" || s.Properties[1].Name != "alpha" {
		t.Errorf("properties lost source order: %+v", s.Properties)
	}

	multi := decodeSchema(t, `{"extends": ["A", "B"], "returns": "string"}`)
	if !slices.Equal(multi.Extends, []string{"A", "B"}) {
		t.Errorf("extends array = %v", multi.Extends)
	}
	if multi.Returns == nil || multi.Returns.Type != "string" {
		t.Errorf("bare returns type not decoded: %+v", multi.Returns)
	}
}

func TestParseVersion(t *testing.T) {
	v, err := parseVersion("JSONRPC_VERSION 13.1000.0\n")
	if err != nil || v != (APIVersion{13, 1000, 0}) {
		t.Fatalf("parseVersion = %v, %v", v, err)
	}
	if _, err := parseVersion("13.5"); err == nil {
		t.Error("expected error for malformed version")
	}
}

func TestHoistInlineIDs(t *testing.T) {
	spec := &Spec{
		Types: map[string]*Schema{},
		Notifications: map[string]*Schema{
			"A.OnX": decodeSchema(t, `{"params": [{"name": "data", "type": "object", "properties": {
				"type": {"type": "string", "id": "A.Kind", "enum": ["x", "y"], "required": true}}}]}`),
		},
	}
	if err := hoistInlineIDs(spec); err != nil {
		t.Fatal(err)
	}
	def, ok := spec.Types["A.Kind"]
	if !ok || len(def.Enum) != 2 || def.Required {
		t.Fatalf("A.Kind not hoisted correctly: %+v", def)
	}
	prop := spec.Notifications["A.OnX"].Params[0].Properties.Get("type")
	if prop.Ref != "A.Kind" || !prop.Required {
		t.Errorf("inline definition not replaced by a required reference: %+v", prop)
	}
}

func TestNaming(t *testing.T) {
	tests := []struct {
		fn       func(string) string
		in, want string
	}{
		{typeName, "Video.Fields.Movie", "VideoFieldsMovie"},
		{typeName, "Library.Id", "LibraryID"},
		{typeName, "PVR.Details.ChannelGroup", "PVRDetailsChannelGroup"},
		{typeName, "JSONRPC.Ping", "JSONRPCPing"},
		{fieldName, "movieid", "MovieID"},
		{fieldName, "id", "ID"},
		{fieldName, "channeluid", "ChannelUID"},
		{fieldName, "top250", "Top250"},
		{fieldName, "minimumItems", "MinimumItems"},
		{constPart, "-16", "Minus16"},
		{constPart, "anaglyph_cyan_red", "AnaglyphCyanRed"},
		{constPart, "", "Empty"},
		{constPart, "4", "4"},
	}
	for _, tt := range tests {
		if got := tt.fn(tt.in); got != tt.want {
			t.Errorf("%q -> %q, want %q", tt.in, got, tt.want)
		}
	}
}
