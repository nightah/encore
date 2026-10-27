package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/store"
)

func info(ctx context.Context) *reqInfo {
	ri, _ := ctx.Value(ctxKey{}).(*reqInfo)
	if ri == nil {
		return &reqInfo{}
	}

	return ri
}

func (a *API) middleware(ctx huma.Context, next func(huma.Context)) {
	r, w := humago.Unwrap(ctx)
	op := ctx.Operation()

	w.Header().Set("Cache-Control", "no-store")

	principal, err := a.auth.Authenticate(w, r)
	public := op.Metadata[metaPublic] == true

	switch {
	case public:
	case errors.Is(err, auth.ErrNotAdmin):
		_ = huma.WriteErr(a.humaAPI, ctx, http.StatusForbidden, err.Error())

		return
	case err != nil:
		a.log.Error("authenticating request", "path", r.URL.Path, "error", err)
		_ = huma.WriteErr(a.humaAPI, ctx, http.StatusInternalServerError, "internal error")

		return
	case principal == nil:
		_ = huma.WriteErr(a.humaAPI, ctx, http.StatusUnauthorized, "sign in first")

		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if !a.auth.CheckCSRF(r) {
			_ = huma.WriteErr(a.humaAPI, ctx, http.StatusForbidden, "missing or invalid "+auth.CSRFHeader+" header; fetch one from GET /api/v1/auth/state")

			return
		}
	}

	next(huma.WithValue(ctx, ctxKey{}, &reqInfo{r: r, w: w, principal: principal}))
}

// protected marks an operation as needing an admin, in the OpenAPI document as well.
func protected(op huma.Operation) huma.Operation {
	op.Security = []map[string][]string{{"session": {}}}

	return op
}

func public(op huma.Operation) huma.Operation {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}

	op.Metadata[metaPublic] = true

	return op
}

// problemFor maps internal errors onto problem responses. Errors that are already huma status errors pass through; anything unrecognised is logged and reported as a bare 500 so internals do not leak.
func (a *API) problemFor(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	var se huma.StatusError
	if errors.As(err, &se) {
		return err
	}

	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, app.ErrNoRuntime):
		return huma.Error404NotFound("not found")
	case errors.Is(err, store.ErrConflict):
		return huma.Error409Conflict(conflictDetail(err))
	case errors.Is(err, store.ErrInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, context.Canceled):
		return huma.NewError(499, "request cancelled")
	}

	path := ""
	if r := info(ctx).r; r != nil {
		path = r.URL.Path
	}

	a.log.Error("admin API request failed", "path", path, "error", err)

	return huma.Error500InternalServerError("internal error")
}

func conflictDetail(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, store.ErrConflict.Error()+": "); i >= 0 {
		if rest := msg[i+len(store.ErrConflict.Error())+2:]; !strings.Contains(rest, "constraint") {
			return rest
		}
	}

	return "already exists"
}

// fieldError is a 422 for one request field.
func fieldError(location, msg string, value any) error {
	return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: location, Message: msg, Value: value})
}

// writeProblem writes a problem document outside Huma, for the API's 404 fallback.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(huma.ErrorModel{Title: http.StatusText(status), Status: status, Detail: detail})
}

// managedError is the 409 for changing a value set in encore.yml or ENCORE_* variables.
func managedError(key string) error {
	return huma.Error409Conflict(fmt.Sprintf("%s is managed by config (encore.yml or ENCORE_* environment variables) and cannot be changed here", key))
}
