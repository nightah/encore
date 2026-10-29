package docsgen

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// APIEndpoints renders the admin API's endpoints page from the OpenAPI document: a section per tag, in the document's tag order, with the resources its operations return and a row per operation.
func APIEndpoints(document []byte) ([]byte, error) {
	var doc openAPIDoc
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, fmt.Errorf("reading the OpenAPI document: %w", err)
	}

	r := apiRenderer{doc: doc, shown: map[string]bool{}}

	var ops []apiOp

	for path, item := range doc.Paths {
		for method, op := range item {
			ops = append(ops, apiOp{Method: strings.ToUpper(method), Path: path, op: op})
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}

		return methodOrder(ops[i].Method) < methodOrder(ops[j].Method)
	})

	// Every response resource gets one block, in the first section that returns it, and is named everywhere else.
	for _, op := range ops {
		for _, name := range op.op.responseSchemas() {
			r.shown[name] = true
		}
	}

	var sections []APISection

	placed := map[string]bool{}

	for _, tag := range doc.Tags {
		s := APISection{Title: tag.title(), Description: tag.Description}

		for _, op := range ops {
			if !slices.Contains(op.op.Tags, tag.Name) {
				continue
			}

			for _, name := range op.op.responseSchemas() {
				if placed[name] {
					continue
				}

				placed[name] = true
				s.Resources = append(s.Resources, r.resource(name))
			}

			s.Operations = append(s.Operations, r.operation(op))
		}

		if len(s.Operations) > 0 {
			sections = append(sections, s)
		}
	}

	for _, op := range ops {
		if !slices.ContainsFunc(doc.Tags, func(t openAPITag) bool { return slices.Contains(op.op.Tags, t.Name) }) {
			return nil, fmt.Errorf("%s %s has no tag declared in the document", op.Method, op.Path)
		}
	}

	return render("endpoints.md.tmpl", struct {
		Prefix   string
		Sections []APISection
	}{apiPrefix, sections})
}

type apiRenderer struct {
	doc openAPIDoc
	// shown names the schemas that have a resource block of their own, so shapes refer to them by name.
	shown map[string]bool
}

func (r apiRenderer) operation(o apiOp) APIOperation {
	out := APIOperation{
		Method: o.Method,
		Path:   strings.TrimPrefix(o.Path, apiPrefix) + r.query(o.op.Parameters),
		Public: len(o.op.Security) == 0,
		Notes:  sentence(o.op.Summary),
		Params: queryNotes(o.op.Parameters),
	}

	if o.op.Description != "" {
		out.Notes += " " + sentence(o.op.Description)
	}

	if b := o.op.RequestBody; b != nil {
		for media, c := range b.Content {
			if media == "application/json" {
				out.Request = r.shape(c.Schema, 1, true)
				out.Params = append(out.Params, r.bodyNotes(c.Schema)...)
			} else {
				out.Request = media
			}
		}
	}

	out.Response = r.response(o.op)

	return out
}

// query lists an operation's query parameters as they are written in a URL, with their defaults.
func (r apiRenderer) query(params []openAPIParam) string {
	var q []string

	for _, p := range params {
		if p.In != "query" {
			continue
		}

		v := ""
		if p.Schema != nil && p.Schema.Default != nil {
			v = fmt.Sprint(p.Schema.Default)
		}

		q = append(q, p.Name+"="+v)
	}

	if len(q) == 0 {
		return ""
	}

	return "?" + strings.Join(q, "&")
}

// queryNotes are the query parameters that say something about themselves: a description or the range they take.
func queryNotes(params []openAPIParam) []APIField {
	var out []APIField

	for _, p := range params {
		if p.In != "query" || p.Schema == nil {
			continue
		}

		doc := p.Schema.Description
		if p.Schema.Minimum != nil && p.Schema.Maximum != nil {
			bounds := fmt.Sprintf("%v to %v", *p.Schema.Minimum, *p.Schema.Maximum)
			if doc == "" {
				doc = "From " + bounds
			} else {
				doc += " (" + bounds + ")"
			}
		}

		if doc != "" {
			out = append(out, APIField{Path: p.Name, Doc: sentence(doc)})
		}
	}

	return out
}

// bodyNotes are the documented top-level fields of a request body.
func (r apiRenderer) bodyNotes(s *openAPISchema) []APIField {
	var out []APIField

	if _, t := r.resolve(s); t != nil {
		for _, key := range t.keys() {
			p := t.Properties[key]

			d := sentence(p.description())
			if p.Default != nil {
				d = strings.TrimSpace(d + " Defaults to `" + fmt.Sprint(p.Default) + "`.")
			}

			if d != "" {
				out = append(out, APIField{Path: key, Doc: d})
			}
		}
	}

	return out
}

// response is the success status and what it carries: a resource's name, a shape, or a media type.
func (r apiRenderer) response(op openAPIOp) string {
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		if strings.HasPrefix(code, "2") {
			codes = append(codes, code)
		}
	}

	sort.Strings(codes)

	if len(codes) == 0 {
		return ""
	}

	code := codes[0]
	res := op.Responses[code]

	var body string

	for media, c := range res.Content {
		if media == "application/json" {
			body = r.shape(c.Schema, 2, false)
		} else {
			body = media
		}
	}

	switch {
	case body == "":
		return code
	case code == "200":
		return body
	default:
		return code + " " + body
	}
}

func (r apiRenderer) resource(name string) APIResource {
	s := r.doc.Components.Schemas[name]
	res := APIResource{Name: name}

	for _, key := range s.keys() {
		res.Lines = append(res.Lines, r.property(key, s.Properties[key], 2, false))
	}

	res.Fields = r.fields("", s, map[string]bool{name: true})

	return res
}

// fields lists the documented fields of s and of the objects inside it that have no resource block of their own, by their dotted path.
func (r apiRenderer) fields(prefix string, s *openAPISchema, seen map[string]bool) []APIField {
	var out []APIField

	for _, key := range s.keys() {
		p := s.Properties[key]
		path := prefix + key

		if d := p.description(); d != "" {
			out = append(out, APIField{Path: path, Doc: sentence(d)})
		}

		inner, name := r.object(p)
		if inner == nil || seen[name] || (name != "" && r.shown[name]) {
			continue
		}

		suffix := "."
		if p.isArray() {
			suffix = "[]."
		}

		next := with(seen, name)
		out = append(out, r.fields(path+suffix, inner, next)...)
	}

	return out
}

// object returns the object schema s holds, directly or as an array's items, and its component name if it has one.
func (r apiRenderer) object(s *openAPISchema) (*openAPISchema, string) {
	if s == nil {
		return nil, ""
	}

	if s.isArray() {
		return r.object(s.Items)
	}

	if inner, ok := s.orNull(); ok {
		return r.object(inner)
	}

	name, t := r.resolve(s)
	if t == nil || len(t.Properties) == 0 {
		return nil, ""
	}

	return t, name
}

func (r apiRenderer) resolve(s *openAPISchema) (string, *openAPISchema) {
	if s.Ref == "" {
		return "", s
	}

	name := strings.TrimPrefix(s.Ref, "#/components/schemas/")

	return name, r.doc.Components.Schemas[name]
}

// shape writes s compactly: objects as their keys, nested objects up to depth levels, enums as their values, and resources with a block of their own by name. With optional, keys a request may leave out end in ?.
func (r apiRenderer) shape(s *openAPISchema, depth int, optional bool) string {
	if s == nil {
		return ""
	}

	if inner, ok := s.orNull(); ok {
		if v := r.shape(inner, depth, optional); v != "" {
			return v + " | null"
		}

		return ""
	}

	if s.isArray() {
		item := r.shape(s.Items, depth, optional)
		if item == "" {
			item = "…"
		}

		return "[" + item + "]"
	}

	name, t := r.resolve(s)

	switch {
	case t == nil:
		return name
	case name != "" && r.shown[name] && !optional:
		return name
	case len(t.Enum) > 0:
		return `"` + enumValues(t.Enum) + `"`
	case len(t.Properties) == 0:
		if !t.isObject() || t.AdditionalProperties == nil {
			return ""
		}

		var values openAPISchema
		if json.Unmarshal(t.AdditionalProperties, &values) != nil {
			return "{}"
		}

		if v := r.shape(&values, depth, optional); v != "" {
			return `{"…": ` + v + "}"
		}

		return "{}"
	case depth == 0:
		return "{…}"
	}

	parts := make([]string, 0, len(t.Properties))
	for _, key := range t.keys() {
		parts = append(parts, r.property(key, t.Properties[key], depth-1, optional && !slices.Contains(t.Required, key)))
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

func (r apiRenderer) property(key string, p *openAPISchema, depth int, optional bool) string {
	out := strconv.Quote(key)
	if optional {
		out += "?"
	}

	if v := r.shape(p, depth, optional); v != "" {
		out += ": " + v
	}

	return out
}

// sentence ends s with a full stop unless it already ends a sentence.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s[len(s)-1:], ".!?:") {
		return s
	}

	return s + "."
}

func enumValues(values []any) string {
	out := make([]string, 0, len(values))

	for _, v := range values {
		if s := fmt.Sprint(v); s != "" {
			out = append(out, s)
		}
	}

	return strings.Join(out, "|")
}

func methodOrder(m string) int {
	return slices.Index([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, m)
}

func with(seen map[string]bool, name string) map[string]bool {
	out := make(map[string]bool, len(seen)+1)
	for k, v := range seen {
		out[k] = v
	}

	if name != "" {
		out[name] = true
	}

	return out
}

func (s *openAPISchema) keys() []string {
	// The document lists required fields in declaration order; the rest follow alphabetically.
	keys := slices.Clone(s.Required)

	var rest []string

	for k := range s.Properties {
		if !slices.Contains(keys, k) {
			rest = append(rest, k)
		}
	}

	sort.Strings(rest)

	return append(keys, rest...)
}

// orNull returns the schema a nullable oneOf holds besides null.
func (s *openAPISchema) orNull() (*openAPISchema, bool) {
	if len(s.OneOf) != 2 {
		return nil, false
	}

	for i, o := range s.OneOf {
		if o.hasType("null") {
			return s.OneOf[1-i], true
		}
	}

	return nil, false
}

func (s *openAPISchema) isArray() bool { return s.hasType("array") }

func (s *openAPISchema) isObject() bool { return s.hasType("object") }

func (s *openAPISchema) hasType(t string) bool {
	switch v := s.Type.(type) {
	case string:
		return v == t
	case []any:
		return slices.Contains(v, any(t))
	}

	return false
}

func (s *openAPISchema) description() string {
	if s.Description != "" {
		return s.Description
	}

	if s.isArray() && s.Items != nil {
		return s.Items.Description
	}

	return ""
}

func (o openAPIOp) responseSchemas() []string {
	var out []string

	for code, res := range o.Responses {
		if !strings.HasPrefix(code, "2") {
			continue
		}

		for media, c := range res.Content {
			if media != "application/json" || c.Schema == nil {
				continue
			}

			s := c.Schema
			if s.isArray() && s.Items != nil {
				s = s.Items
			}

			if name := strings.TrimPrefix(s.Ref, "#/components/schemas/"); s.Ref != "" {
				out = append(out, name)
			}
		}
	}

	return out
}

func (t openAPITag) title() string {
	if v, ok := t.DisplayName.(string); ok && v != "" {
		return v
	}

	return t.Name
}
