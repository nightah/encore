package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// dropped counts the reports refused in the previous window.
func (l *clientErrorLimiter) allow(now time.Time) (ok bool, dropped int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.window) >= time.Minute {
		dropped = l.dropped
		l.window, l.count, l.dropped = now, 0, 0
	}

	if l.count >= clientErrorsPerMinute {
		l.dropped++

		return false, dropped
	}

	l.count++

	return true, dropped
}

func (a *API) registerClientErrors(api huma.API) {
	// Public, so errors on the setup and sign-in pages are logged too; same-origin protection and the CSRF token still apply, so only pages this server served can report.
	huma.Register(api, public(huma.Operation{
		OperationID: "reportClientError", Method: http.MethodPost, Path: Prefix + "/client-errors", Tags: []string{"auth"},
		Summary: "Report an error the web interface hit", Description: "Logged at warn level on the server, at most 20 a minute.", DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *struct{ Body ClientErrorReport }) (*struct{}, error) {
		ok, dropped := a.clientErrors.allow(a.now())
		if dropped > 0 {
			a.log.Warn("browser errors not logged: too many in a minute", "dropped", dropped)
		}

		if !ok {
			return nil, nil
		}

		ri := info(ctx)
		user := ""

		if ri.principal != nil {
			user = ri.principal.Name
		}

		agent := ""
		if ri.r != nil {
			agent = ri.r.UserAgent()
		}

		a.log.Warn("browser error", "kind", in.Body.Kind, "message", in.Body.Message, "path", in.Body.Path, "user", user, "user_agent", agent, "stack", in.Body.Stack)

		return nil, nil
	})
}
