package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// nilable reports whether the zero value of k already encodes as null or is omitted by omitempty.
func (k kind) nilable() bool {
	return k == kindAny || k == kindSlice || k == kindMap || k == kindPointer || k == kindUnion
}

func newGenerator(spec *Spec) *generator {
	return &generator{
		spec:   spec,
		decls:  map[string]decl{},
		owners: map[string]string{},
		done:   map[string]bool{},
		kinds:  map[string]kind{},
	}
}

func (g *generator) errorf(format string, args ...any) {
	g.errs = append(g.errs, fmt.Errorf(format, args...))
}

func (g *generator) err() error { return errors.Join(g.errs...) }

// claim reserves a package-level identifier for origin, reporting an error on collisions between different schema elements.
func (g *generator) claim(name, origin string) bool {
	if prev, ok := g.owners[name]; ok {
		if prev != origin {
			g.errorf("identifier %s is claimed by both %q and %q", name, prev, origin)
		}
		return false
	}
	g.owners[name] = origin
	return true
}

// claimUnique reserves name, or name with the smallest numeric suffix that is free, and returns the reserved identifier.
func (g *generator) claimUnique(name, origin string) string {
	candidate := name
	for i := 2; ; i++ {
		if _, taken := g.owners[candidate]; !taken {
			g.owners[candidate] = origin
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", name, i)
	}
}

// enter makes name the parent of inline declarations until the returned function is called.
func (g *generator) enter(name string) func() {
	prev := g.parent
	g.parent = name
	return func() { g.parent = prev }
}

func (g *generator) emit(name, code string) {
	g.decls[name] = decl{file: g.file, code: code}
}

// simpleType normalises a "type" keyword value, returning "" when it is not one of the draft-03 primitive names (Kodi sometimes writes "Integer" or puts a type id there).
func simpleType(t string) string {
	switch lt := strings.ToLower(t); lt {
	case "string", "integer", "number", "boolean", "object", "array", "any", "null":
		return lt
	}
	return ""
}

// refOf returns the referenced type id of s, treating a type id used as "type" as a reference.
func (g *generator) refOf(s *Schema) string {
	if s.Ref != "" {
		return s.Ref
	}
	if s.Type != "" && simpleType(s.Type) == "" {
		if _, ok := g.spec.Types[s.Type]; ok {
			return s.Type
		}
	}
	return ""
}

func splitNull(union []*Schema) (alts []*Schema, nullable bool) {
	for _, alt := range union {
		if alt.Ref == "" && simpleType(alt.Type) == "null" && len(alt.Union) == 0 {
			nullable = true
			continue
		}
		alts = append(alts, alt)
	}
	return alts, nullable
}

// effectiveType inherits through "extends" when s does not declare a type.
func (g *generator) effectiveType(s *Schema) string {
	if t := simpleType(s.Type); t != "" {
		return t
	}
	for _, parent := range s.Extends {
		if p, ok := g.spec.Types[parent]; ok {
			if t := g.effectiveType(p); t != "" {
				return t
			}
		}
	}
	if len(s.Properties) > 0 {
		return "object"
	}
	return ""
}

// mergeExtends overlays s onto its (recursively merged) parents for non-object schemas, e.g. an array of field names extending Item.Fields.Base. Keys set on s win.
func (g *generator) mergeExtends(s *Schema) *Schema {
	if len(s.Extends) == 0 {
		return s
	}
	merged := &Schema{}
	for _, id := range s.Extends {
		p, ok := g.spec.Types[id]
		if !ok {
			g.errorf("extends unknown type %q", id)
			continue
		}
		overlay(merged, g.mergeExtends(p))
	}
	own := *s
	own.Extends = nil
	overlay(merged, &own)
	return merged
}

// overlay merges properties by name, keeping first-seen order.
func overlay(dst, src *Schema) {
	if src.Ref != "" {
		dst.Ref = src.Ref
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.Type != "" || src.Union != nil {
		dst.Type, dst.Union = src.Type, src.Union
	}
	if src.Enum != nil {
		dst.Enum = src.Enum
	}
	if src.Default != nil {
		dst.Default = src.Default
	}
	if src.Items != nil {
		dst.Items = src.Items
	}
	if src.AdditionalProperties != nil {
		dst.AdditionalProperties = src.AdditionalProperties
	}
	dst.Required = dst.Required || src.Required
	for _, p := range src.Properties {
		if i := slices.IndexFunc(dst.Properties, func(q Prop) bool { return q.Name == p.Name }); i >= 0 {
			dst.Properties[i] = p
		} else {
			dst.Properties = append(dst.Properties, p)
		}
	}
}

func (g *generator) kindOfID(id string) kind {
	if k, ok := g.kinds[id]; ok {
		return k
	}
	s, ok := g.spec.Types[id]
	if !ok {
		return kindAny
	}
	g.kinds[id] = kindAny // guards pure reference cycles
	k := g.kindOf(s)
	g.kinds[id] = k
	return k
}

func (g *generator) kindOf(s *Schema) kind {
	if ref := g.refOf(s); ref != "" {
		return g.kindOfID(ref)
	}
	if s.Union != nil {
		alts, nullable := splitNull(s.Union)
		switch len(alts) {
		case 0:
			return kindAny
		case 1:
			k := g.kindOf(alts[0])
			if nullable && !k.nilable() {
				return kindPointer
			}
			return k
		default:
			return kindUnion
		}
	}
	if len(s.Enum) > 0 {
		return kindScalar
	}
	switch g.effectiveType(s) {
	case "string", "integer", "number", "boolean":
		return kindScalar
	case "array":
		return kindSlice
	case "object":
		if len(s.Properties) > 0 || len(s.Extends) > 0 {
			return kindStruct
		}
		return kindMap
	}
	return kindAny
}

func (g *generator) ensureType(id string) string {
	name := typeName(id)
	if g.done[id] {
		return name
	}
	g.done[id] = true
	s, ok := g.spec.Types[id]
	if !ok {
		g.errorf("reference to unknown type %q", id)
		return "any"
	}
	if g.owners[name] == id || g.claim(name, id) {
		g.define(name, s, fmt.Sprintf("%s is the Kodi type %s.", name, id))
	}
	return name
}

// typeOf turns inline structs, enums and unions into named declarations called ctx.
func (g *generator) typeOf(s *Schema, ctx string) goType {
	if s == nil {
		return goType{"any", kindAny}
	}
	if ref := g.refOf(s); ref != "" {
		return goType{g.ensureType(ref), g.kindOfID(ref)}
	}

	if s.Union != nil {
		alts, nullable := splitNull(s.Union)
		switch len(alts) {
		case 0:
			return goType{"any", kindAny}
		case 1:
			t := g.typeOf(alts[0], ctx)
			if nullable && !t.kind.nilable() {
				return goType{"*" + t.expr, kindPointer}
			}
			return t
		}
		return g.defineInline(s, ctx, kindUnion)
	}

	if k := g.kindOf(s); k == kindStruct {
		return g.defineInline(s, ctx, k)
	}
	s = g.mergeExtends(s)
	if len(s.Enum) > 0 {
		return g.defineInline(s, ctx, kindScalar)
	}

	switch g.effectiveType(s) {
	case "string":
		return goType{"string", kindScalar}
	case "integer":
		return goType{"int", kindScalar}
	case "number":
		return goType{"float64", kindScalar}
	case "boolean":
		return goType{"bool", kindScalar}
	case "array":
		return goType{"[]" + g.typeOf(s.Items, ctx+"Item").expr, kindSlice}
	case "object":
		if s.AdditionalProperties != nil {
			return goType{"map[string]" + g.typeOf(s.AdditionalProperties, ctx+"Value").expr, kindMap}
		}
		return goType{"map[string]any", kindMap}
	}
	return goType{"any", kindAny}
}

func (g *generator) defineInline(s *Schema, ctx string, k kind) goType {
	name := g.claimUnique(ctx, "inline:"+ctx)
	g.define(name, s, fmt.Sprintf("%s is an inline type of %s.", name, g.parent))
	return goType{name, k}
}

// define emits a named declaration for s called name. lead is the first sentence of its doc comment; the schema description follows it.
func (g *generator) define(name string, s *Schema, lead string) {
	defer g.enter(name)()

	if s == nil {
		g.emit(name, docComment(lead, "")+fmt.Sprintf("type %s = any\n", name))
		return
	}
	doc := docComment(lead, s.Description)

	if ref := g.refOf(s); ref != "" {
		g.emit(name, fmt.Sprintf("%stype %s = %s\n", doc, name, g.ensureType(ref)))
		return
	}

	if s.Union != nil {
		alts, nullable := splitNull(s.Union)
		if len(alts) >= 2 {
			g.emit(name, doc+g.renderUnion(name, alts, nullable))
			return
		}
		t := g.typeOf(s, name+"Value")
		g.emit(name, fmt.Sprintf("%stype %s = %s\n", doc, name, t.expr))
		return
	}

	switch g.kindOf(s) {
	case kindStruct:
		embeds, props := g.objectLayout(s)
		g.emit(name, doc+g.renderStruct(name, name, embeds, props))
		return
	case kindAny:
		g.emit(name, fmt.Sprintf("%stype %s = any\n", doc, name))
		return
	}

	merged := g.mergeExtends(s)
	if len(merged.Enum) > 0 {
		g.emit(name, doc+g.renderEnum(name, merged))
		return
	}
	// Scalars, arrays and maps become defined types; array elements and map values that need their own declaration are named name+"Item" and name+"Value".
	t := g.typeOf(merged, name)
	g.emit(name, fmt.Sprintf("%stype %s %s\n", doc, name, t.expr))
}

// objectLayout decides how an object schema's inheritance is expressed: a single object parent is embedded; multiple parents are flattened, because encoding/json silently drops fields that two embedded structs both provide at the same depth (the Kodi hierarchy has diamonds).
func (g *generator) objectLayout(s *Schema) (embeds []string, props Props) {
	if len(s.Extends) == 1 {
		if g.kindOfID(s.Extends[0]) == kindStruct {
			return s.Extends, s.Properties
		}
	}
	return nil, g.mergeExtends(s).Properties
}

// renderStruct prefixes the names of inline field types with ctx.
func (g *generator) renderStruct(name, ctx string, embeds []string, props Props) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type %s struct {\n", name)

	used := map[string]bool{}
	for _, id := range embeds {
		parent := g.ensureType(id)
		used[parent] = true
		fmt.Fprintf(&b, "\t%s\n", parent)
	}
	for _, p := range props {
		field := fieldName(p.Name)
		for i := 2; used[field]; i++ {
			field = fmt.Sprintf("%s%d", fieldName(p.Name), i)
		}
		used[field] = true
		b.WriteString(g.renderField(field, ctx+field, p.Name, p.Schema))
	}
	b.WriteString("}\n")
	return b.String()
}

// renderField renders one struct field. Optional fields are omitted from the encoding when unset:
//   - structs become pointers (presence is observable on decode),
//   - unions use omitzero (they implement IsZero),
//   - scalars whose schema default differs from the Go zero value become pointers, so the zero value can still be sent explicitly,
//   - everything else uses omitempty.
func (g *generator) renderField(field, ctx, jsonName string, s *Schema) string {
	t := g.typeOf(s, ctx)
	expr, tag := t.expr, jsonName

	if !s.Required {
		switch {
		case t.kind == kindStruct:
			expr, tag = "*"+expr, tag+",omitempty"
		case t.kind == kindUnion:
			tag += ",omitzero"
		case t.kind == kindScalar && g.zeroIsMeaningful(s):
			expr, tag = "*"+expr, tag+",omitempty"
		default:
			tag += ",omitempty"
		}
	}

	var b strings.Builder
	if d := sentence(s.Description); d != "" {
		b.WriteString(strings.ReplaceAll(wrapComment(d), "// ", "\t// "))
	}
	fmt.Fprintf(&b, "\t%s %s `json:%q`\n", field, expr, tag)
	return b.String()
}

// zeroIsMeaningful reports whether omitting the Go zero value of an optional scalar would change its meaning: the property has an explicit non-zero default and the zero value is a legal value.
func (g *generator) zeroIsMeaningful(s *Schema) bool {
	def := bytes.TrimSpace(s.Default)
	switch string(def) {
	case "", "null", "false", "0", `""`:
		return false
	}
	target := s
	if ref := g.refOf(s); ref != "" {
		target = g.spec.Types[ref]
	}
	if target == nil || len(target.Enum) == 0 {
		return true
	}
	for _, v := range target.Enum {
		switch string(bytes.TrimSpace(v)) {
		case "0", `""`:
			return true
		}
	}
	return false
}

func (g *generator) renderEnum(name string, s *Schema) string {
	base := "string"
	if t := g.effectiveType(s); t == "integer" || t == "number" {
		base = "int"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "type %s %s\n\n", name, base)
	fmt.Fprintf(&b, "// %s values.\nconst (\n", name)
	for _, raw := range s.Enum {
		var lit, part string
		if base == "string" {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				g.errorf("%s: enum value %s: %v", name, raw, err)
				continue
			}
			lit, part = fmt.Sprintf("%q", v), constPart(v)
		} else {
			var v int
			if err := json.Unmarshal(raw, &v); err != nil {
				g.errorf("%s: enum value %s: %v", name, raw, err)
				continue
			}
			lit, part = fmt.Sprint(v), constPart(fmt.Sprint(v))
		}
		c := g.claimUnique(name+part, name)
		fmt.Fprintf(&b, "\t%s %s = %s\n", c, name, lit)
	}
	b.WriteString(")\n")
	return b.String()
}

// renderUnion's wrapper embeds the hand-written union helper.
func (g *generator) renderUnion(name string, alts []*Schema, nullable bool) string {
	variants := make([]unionVariant, 0, len(alts))
	labels := map[string]bool{}
	for _, alt := range alts {
		base := g.variantLabel(alt)
		label := base
		for i := 2; labels[label]; i++ {
			label = fmt.Sprintf("%s%d", base, i)
		}
		labels[label] = true
		variants = append(variants, unionVariant{label, g.typeOf(alt, name+label)})
	}

	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = v.typ.expr
	}
	if nullable {
		names = append(names, "null")
	}

	var b strings.Builder
	b.WriteString("//\n" + wrapComment("It holds one of: "+strings.Join(names, ", ")+"."))
	fmt.Fprintf(&b, "type %s struct{ union }\n", name)
	for _, v := range variants {
		ctor := g.claimUnique("New"+name+"From"+v.label, name)
		fmt.Fprintf(&b, "\n// %s returns a %s holding v.\n", ctor, name)
		fmt.Fprintf(&b, "func %s(v %s) %s { return %s{newUnion(v)} }\n", ctor, v.typ.expr, name, name)
		fmt.Fprintf(&b, "\n// As%s decodes the value as %s.\n", v.label, v.typ.expr)
		fmt.Fprintf(&b, "func (u %s) As%s() (%s, error) { return unionAs[%s](u.union) }\n",
			name, v.label, v.typ.expr, v.typ.expr)
	}
	return b.String()
}

// variantLabel names a union alternative for constructor and accessor names: the referenced type, the primitive kind, or for inline objects the names of their required properties.
func (g *generator) variantLabel(s *Schema) string {
	if ref := g.refOf(s); ref != "" {
		return typeName(ref)
	}
	if s.Union != nil {
		return "Union"
	}
	switch g.effectiveType(s) {
	case "string":
		return "String"
	case "integer":
		return "Int"
	case "number":
		return "Float"
	case "boolean":
		return "Bool"
	case "array":
		return "Array"
	case "object":
		var parts []string
		for _, p := range g.mergeExtends(s).Properties {
			if p.Schema.Required {
				parts = append(parts, fieldName(p.Name))
			}
		}
		if len(parts) == 0 && len(s.Properties) > 0 {
			parts = append(parts, fieldName(s.Properties[0].Name))
		}
		if len(parts) == 0 {
			return "Object"
		}
		return strings.Join(parts, "")
	}
	return "Any"
}
