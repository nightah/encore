package basepath

import (
	"context"
	"strings"
)

// Clean returns base with one leading and one trailing slash, e.g. "/encore/", or "/" for none.
func Clean(base string) string {
	base = strings.Trim(base, "/")
	if base == "" {
		return "/"
	}

	return "/" + base + "/"
}

// With records that the request arrived under base.
func With(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, contextKey{}, Clean(base))
}

// From returns the prefix recorded by With, ending in a slash; "/" when there is none.
func From(ctx context.Context) string {
	if base, ok := ctx.Value(contextKey{}).(string); ok {
		return base
	}

	return "/"
}
