package main

import (
	"fmt"
	"slices"
	"strings"
)

// methodStem returns the Go identifier stem of a method or notification ("VideoLibrary.GetMovies" -> "VideoLibraryGetMovies").
func methodStem(name string) string { return typeName(name) }

func paramProps(params []*Schema) Props {
	props := make(Props, 0, len(params))
	for _, p := range params {
		props = append(props, Prop{Name: p.Name, Schema: p})
	}
	return props
}

func (g *generator) genMethods() {
	g.file = "methods"
	for _, name := range sortedKeys(g.spec.Methods) {
		m := g.spec.Methods[name]
		stem := methodStem(name)

		params := stem + "Params"
		if g.claim(params, name) {
			doc := wrapComment(fmt.Sprintf("%s holds the parameters of %s, sent as a JSON object keyed by parameter name.", params, name))
			done := g.enter(params)
			g.emit(params, doc+g.renderStruct(params, stem, nil, paramProps(m.Params)))
			done()
		}

		result := stem + "Result"
		if g.claim(result, name) {
			g.define(result, m.Returns, fmt.Sprintf("%s is the result of %s.", result, name))
		}
	}
}

func (g *generator) genNotifications() {
	g.file = "notifications"
	for _, name := range sortedKeys(g.spec.Notifications) {
		n := g.spec.Notifications[name]
		stem := methodStem(name)
		payload := stem + "Notification"
		if !g.claim(payload, name) {
			continue
		}
		doc := docComment(fmt.Sprintf("%s is the payload (params) of the %s notification.", payload, name), n.Description)
		done := g.enter(payload)
		g.emit(payload, doc+g.renderStruct(payload, stem, nil, paramProps(n.Params)))
		done()
	}
}

// renderMethodTable also renders the typed Client wrappers.
func (g *generator) renderMethodTable(compat *Spec) string {
	names := sortedKeys(g.spec.Methods)
	var b strings.Builder

	b.WriteString("// Method names.\nconst (\n")
	for _, name := range names {
		c := "Method" + methodStem(name)
		g.claim(c, name)
		fmt.Fprintf(&b, "\t%s = %q\n", c, name)
	}
	b.WriteString(")\n\n")

	fmt.Fprintf(&b, "// SchemaVersion is the API version of the schema this package was generated from.\n")
	fmt.Fprintf(&b, "var SchemaVersion = %s\n\n", versionLit(g.spec.Version))
	base := g.spec.Version
	if compat != nil {
		base = compat.Version
	}
	fmt.Fprintf(&b, "// BaselineVersion is the oldest API version the availability data covers.\n")
	fmt.Fprintf(&b, "var BaselineVersion = %s\n\n", versionLit(base))

	b.WriteString("// Methods describes every method of the schema, keyed by method name.\n")
	b.WriteString("var Methods = map[string]MethodInfo{\n")
	for _, name := range names {
		m := g.spec.Methods[name]
		fmt.Fprintf(&b, "\tMethod%s: {\n", methodStem(name))
		fmt.Fprintf(&b, "\t\tName: %q,\n", name)
		if d := strings.Join(strings.Fields(m.Description), " "); d != "" {
			fmt.Fprintf(&b, "\t\tDescription: %q,\n", d)
		}
		if m.Permission != "" {
			fmt.Fprintf(&b, "\t\tPermission: %q,\n", m.Permission)
		}
		if len(m.Transport) > 0 {
			fmt.Fprintf(&b, "\t\tTransport: %s,\n", stringsLit(m.Transport))
		}
		if len(m.Params) > 0 {
			names := make([]string, len(m.Params))
			for i, p := range m.Params {
				names[i] = p.Name
			}
			fmt.Fprintf(&b, "\t\tParams: %s,\n", stringsLit(names))
		}
		since, paramsSince := availability(name, m, g.spec, compat)
		fmt.Fprintf(&b, "\t\tSince: %s,\n", versionLit(since))
		if len(paramsSince) > 0 {
			b.WriteString("\t\tParamsSince: map[string]APIVersion{\n")
			for _, p := range paramsSince {
				fmt.Fprintf(&b, "\t\t\t%q: %s,\n", p, strings.TrimPrefix(versionLit(g.spec.Version), "APIVersion"))
			}
			b.WriteString("\t\t},\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// availability returns the version a method is known to exist since and the parameters that are newer than the method itself. With no compat schema everything is attributed to the main schema's version.
func availability(name string, m *Schema, spec, compat *Spec) (APIVersion, []string) {
	if compat == nil {
		return spec.Version, nil
	}
	old, ok := compat.Methods[name]
	if !ok {
		return spec.Version, nil
	}
	var added []string
	for _, p := range m.Params {
		if !slices.ContainsFunc(old.Params, func(q *Schema) bool { return q.Name == p.Name }) {
			added = append(added, p.Name)
		}
	}
	slices.Sort(added)
	return compat.Version, added
}

func stringsLit(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

func versionLit(v APIVersion) string {
	return fmt.Sprintf("APIVersion{%d, %d, %d}", v.Major, v.Minor, v.Patch)
}

func (g *generator) renderClient() string {
	var b strings.Builder
	for _, name := range sortedKeys(g.spec.Methods) {
		m := g.spec.Methods[name]
		stem := methodStem(name)
		params, result := stem+"Params", stem+"Result"

		b.WriteString(wrapComment(fmt.Sprintf("%s calls %s. %s", stem, name, sentence(m.Description))))
		if len(m.Params) == 0 {
			fmt.Fprintf(&b, "func (c Client) %s(ctx context.Context) (%s, error) {\n", stem, result)
			fmt.Fprintf(&b, "\treturn Call[%s, %s](ctx, c.Caller, Method%s, %s{})\n}\n\n", params, result, stem, params)
			continue
		}
		fmt.Fprintf(&b, "func (c Client) %s(ctx context.Context, p %s) (%s, error) {\n", stem, params, result)
		fmt.Fprintf(&b, "\treturn Call[%s, %s](ctx, c.Caller, Method%s, p)\n}\n\n", params, result, stem)
	}
	return b.String()
}

// renderNotificationTable includes a payload factory for decoding incoming notifications.
func (g *generator) renderNotificationTable() string {
	names := sortedKeys(g.spec.Notifications)
	var b strings.Builder

	b.WriteString("// NotificationName is the method name of a Kodi notification.\n")
	b.WriteString("type NotificationName string\n\n")
	b.WriteString("// Notification names.\nconst (\n")
	for _, name := range names {
		c := "Notification" + methodStem(name)
		g.claim(c, name)
		fmt.Fprintf(&b, "\t%s NotificationName = %q\n", c, name)
	}
	b.WriteString(")\n\n")

	b.WriteString("// Notifications lists every notification name in sorted order.\n")
	b.WriteString("var Notifications = []NotificationName{\n")
	for _, name := range names {
		fmt.Fprintf(&b, "\tNotification%s,\n", methodStem(name))
	}
	b.WriteString("}\n\n")

	b.WriteString("// NewNotificationPayload returns a pointer to a zero payload struct for\n")
	b.WriteString("// the named notification, suitable for json.Unmarshal of its params.\n")
	b.WriteString("func NewNotificationPayload(name NotificationName) (any, bool) {\n\tswitch name {\n")
	for _, name := range names {
		stem := methodStem(name)
		fmt.Fprintf(&b, "\tcase Notification%s:\n\t\treturn new(%sNotification), true\n", stem, stem)
	}
	b.WriteString("\t}\n\treturn nil, false\n}\n")
	return b.String()
}
