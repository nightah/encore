package main

import "encoding/json"

// Spec is one version of the Kodi JSON-RPC schema.
type Spec struct {
	Version       APIVersion
	Types         map[string]*Schema
	Methods       map[string]*Schema
	Notifications map[string]*Schema
}

// APIVersion is a JSON-RPC API version as found in version.txt.
type APIVersion struct {
	Major, Minor, Patch int
}

// Schema is a node of Kodi's JSON-RPC schema, a JSON Schema draft-03 dialect. Only the keywords the generator needs are decoded.
type Schema struct {
	Ref         string
	ID          string
	Name        string // method/notification parameter name
	Description string
	Required    bool

	// Type is the simple type name ("string", "object", ...) or, when Kodi misuses the keyword, a type id. Empty when Union is set or the type is inherited through Extends.
	Type string
	// Union holds the alternatives when "type" is an array. Plain type names in the array are represented as schemas with only Type set.
	Union []*Schema

	Enum       []json.RawMessage
	Default    json.RawMessage // nil when absent
	Extends    []string
	Properties Props
	Items      *Schema

	// AdditionalProperties is the schema of map values, nil when absent or a boolean.
	AdditionalProperties *Schema

	// Method and notification keywords.
	Params     []*Schema
	Returns    *Schema
	Permission string
	Transport  []string
}

// Prop is a named object property; Props keeps the schema's source order.
type Prop struct {
	Name   string
	Schema *Schema
}

// Props is an ordered list of object properties.
type Props []Prop

// looseBool accepts true/false as JSON booleans or strings; Kodi's schema contains a few "required": "true" entries.
type looseBool bool

type generator struct {
	spec *Spec

	file   string            // file new declarations are assigned to
	parent string            // declaration being rendered, for inline docs
	decls  map[string]decl   // keyed by Go identifier
	owners map[string]string // Go identifier -> schema origin
	done   map[string]bool   // type ids already emitted
	kinds  map[string]kind   // memoised kinds of type ids
	errs   []error
}

// kind classifies a generated Go type; it decides how optional fields are declared (pointer, omitempty or omitzero).
type kind int

type goType struct {
	expr string
	kind kind
}

type decl struct {
	file string // output file the declaration belongs to
	code string
}

type unionVariant struct {
	label string
	typ   goType
}
