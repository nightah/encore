package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UnmarshalJSON decodes a JSON object while preserving key order.
func (p *Props) UnmarshalJSON(b []byte) error {
	*p = nil
	return decodeOrdered(b, func(key string, raw json.RawMessage) error {
		s := new(Schema)
		if err := json.Unmarshal(raw, s); err != nil {
			return fmt.Errorf("property %q: %w", key, err)
		}
		*p = append(*p, Prop{Name: key, Schema: s})
		return nil
	})
}

// Get returns the named property schema or nil.
func (p Props) Get(name string) *Schema {
	for _, prop := range p {
		if prop.Name == name {
			return prop.Schema
		}
	}
	return nil
}

// UnmarshalJSON decodes a schema node, normalising the polymorphic keywords ("type", "extends", "returns", "additionalProperties").
func (s *Schema) UnmarshalJSON(b []byte) error {
	var aux struct {
		Ref                  string            `json:"$ref"`
		ID                   string            `json:"id"`
		Name                 string            `json:"name"`
		Description          string            `json:"description"`
		Required             looseBool         `json:"required"`
		Type                 json.RawMessage   `json:"type"`
		Enum                 []json.RawMessage `json:"enum"`
		Default              json.RawMessage   `json:"default"`
		Extends              json.RawMessage   `json:"extends"`
		Properties           Props             `json:"properties"`
		Items                *Schema           `json:"items"`
		AdditionalProperties json.RawMessage   `json:"additionalProperties"`
		Params               []*Schema         `json:"params"`
		Returns              json.RawMessage   `json:"returns"`
		Permission           string            `json:"permission"`
		Transport            json.RawMessage   `json:"transport"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}

	*s = Schema{
		Ref:         aux.Ref,
		ID:          aux.ID,
		Name:        aux.Name,
		Description: aux.Description,
		Required:    bool(aux.Required),
		Enum:        aux.Enum,
		Default:     aux.Default,
		Properties:  aux.Properties,
		Items:       aux.Items,
		Params:      aux.Params,
		Permission:  aux.Permission,
	}

	var err error
	if s.Type, s.Union, err = decodeType(aux.Type); err != nil {
		return fmt.Errorf("type: %w", err)
	}
	if s.Extends, err = decodeStrings(aux.Extends); err != nil {
		return fmt.Errorf("extends: %w", err)
	}
	if s.Transport, err = decodeStrings(aux.Transport); err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	if s.AdditionalProperties, err = decodeSchemaOrScalar(aux.AdditionalProperties); err != nil {
		return fmt.Errorf("additionalProperties: %w", err)
	}
	if s.Returns, err = decodeSchemaOrScalar(aux.Returns); err != nil {
		return fmt.Errorf("returns: %w", err)
	}

	return nil
}

func (b *looseBool) UnmarshalJSON(raw []byte) error {
	switch string(bytes.Trim(raw, `"`)) {
	case "true":
		*b = true
	case "false", "null":
		*b = false
	default:
		return fmt.Errorf("invalid boolean %s", raw)
	}
	return nil
}

// decodeType handles "type": "x" and "type": ["x", {schema}, ...].
func decodeType(raw json.RawMessage) (string, []*Schema, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil, nil
	}
	if raw[0] == '"' {
		var t string
		err := json.Unmarshal(raw, &t)
		return t, nil, err
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", nil, err
	}
	union := make([]*Schema, 0, len(items))
	for _, item := range items {
		s, err := decodeSchemaOrScalar(item)
		if err != nil {
			return "", nil, err
		}
		union = append(union, s)
	}
	return "", union, nil
}

// decodeStrings handles a keyword that is either a string or an array of strings, such as "extends" and "transport".
func decodeStrings(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var id string
		err := json.Unmarshal(raw, &id)
		return []string{id}, err
	}
	var ids []string
	err := json.Unmarshal(raw, &ids)
	return ids, err
}

// decodeSchemaOrScalar decodes a value that is either a schema object, a bare type name (returned as a schema with only Type set), a boolean or null (both returned as nil).
func decodeSchemaOrScalar(raw json.RawMessage) (*Schema, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	switch raw[0] {
	case '{':
		s := new(Schema)
		return s, json.Unmarshal(raw, s)
	case '"':
		var t string
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		return &Schema{Type: t}, nil
	case 't', 'f', 'n':
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected value %s", raw)
}

// decodeOrdered calls fn for each member of a JSON object in source order.
func decodeOrdered(b []byte, fn func(key string, raw json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected object, got %v", tok)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected object key, got %v", tok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := fn(key, raw); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

func (v APIVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// loadSpec reads methods.json, types.json, notifications.json and version.txt from dir.
func loadSpec(dir string) (*Spec, error) {
	spec := &Spec{}

	files := []struct {
		name string
		dst  *map[string]*Schema
	}{
		{"types.json", &spec.Types},
		{"methods.json", &spec.Methods},
		{"notifications.json", &spec.Notifications},
	}
	for _, f := range files {
		m, err := loadSchemaFile(filepath.Join(dir, f.name))
		if err != nil {
			return nil, err
		}
		*f.dst = m
	}

	b, err := os.ReadFile(filepath.Join(dir, "version.txt")) //nolint:gosec // G304: path comes from the -schema flag of a dev tool.
	if err != nil {
		return nil, err
	}
	if spec.Version, err = parseVersion(string(b)); err != nil {
		return nil, fmt.Errorf("version.txt: %w", err)
	}

	if err := hoistInlineIDs(spec); err != nil {
		return nil, err
	}
	addRuntimeEnums(spec)

	return spec, nil
}

// runtimeEnums are string enums that Kodi registers at startup from C++ tables (JSONRPC.cpp, CJSONRPC::Initialize), so they are referenced by the schema files but never defined there. Their values can be listed with JSONRPC.Introspect on a running instance.
var runtimeEnums = []string{
	"Addon.Types",
	"GUI.Window",
	"Input.Action",
	"List.Filter.Fields.Albums",
	"List.Filter.Fields.Artists",
	"List.Filter.Fields.Episodes",
	"List.Filter.Fields.Movies",
	"List.Filter.Fields.MusicVideos",
	"List.Filter.Fields.Songs",
	"List.Filter.Fields.TVShows",
	"List.Filter.Fields.Textures",
	"List.Filter.Operators",
}

func addRuntimeEnums(spec *Spec) {
	for _, id := range runtimeEnums {
		if _, ok := spec.Types[id]; ok {
			continue
		}
		spec.Types[id] = &Schema{
			Type:        "string",
			Description: "is a string enum whose values Kodi registers at runtime; list them with JSONRPC.Introspect.",
		}
	}
}

func loadSchemaFile(path string) (map[string]*Schema, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: path comes from the -schema flag of a dev tool.
	if err != nil {
		return nil, err
	}
	m := map[string]*Schema{}
	err = decodeOrdered(stripComments(b), func(key string, raw json.RawMessage) error {
		s := new(Schema)
		if err := json.Unmarshal(raw, s); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		m[key] = s
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// parseVersion parses "JSONRPC_VERSION 13.5.0".
func parseVersion(s string) (APIVersion, error) {
	fields := strings.Fields(s)
	if len(fields) != 2 || fields[0] != "JSONRPC_VERSION" {
		return APIVersion{}, fmt.Errorf("unexpected content %q", s)
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) != 3 {
		return APIVersion{}, fmt.Errorf("unexpected version %q", fields[1])
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return APIVersion{}, fmt.Errorf("unexpected version %q", fields[1])
		}
		nums[i] = n
	}
	return APIVersion{nums[0], nums[1], nums[2]}, nil
}

// hoistInlineIDs registers schemas that declare an "id" inline (outside types.json) as named types, and replaces each inline definition with a reference so it is generated once.
func hoistInlineIDs(spec *Spec) error {
	var walk func(s *Schema) error
	walk = func(s *Schema) error {
		if s == nil {
			return nil
		}
		for _, c := range children(s) {
			if c.ID != "" && c.Ref == "" {
				if prev, ok := spec.Types[c.ID]; ok && prev != c {
					return fmt.Errorf("inline id %q redefines a type", c.ID)
				}
				def := *c
				def.ID, def.Required, def.Name = "", false, ""
				spec.Types[c.ID] = &def
				*c = Schema{Ref: c.ID, Required: c.Required, Name: c.Name, Description: c.Description}
				if err := walk(&def); err != nil {
					return err
				}
				continue
			}
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}

	for _, group := range []map[string]*Schema{spec.Types, spec.Methods, spec.Notifications} {
		for _, id := range sortedKeys(group) {
			if err := walk(group[id]); err != nil {
				return err
			}
		}
	}
	return nil
}

func children(s *Schema) []*Schema {
	var out []*Schema
	out = append(out, s.Union...)
	for _, p := range s.Properties {
		out = append(out, p.Schema)
	}
	for _, c := range []*Schema{s.Items, s.AdditionalProperties, s.Returns} {
		if c != nil {
			out = append(out, c)
		}
	}
	out = append(out, s.Params...)
	return out
}
