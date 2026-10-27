package alexa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jamestelfer/billy/pkg/alexaverify"
)

// NewVerifier returns the production verifier, which trusts the host's root certificates and has no way to be disabled.
func NewVerifier(log *slog.Logger) (*alexaverify.Verifier, error) {
	return alexaverify.New(alexaverify.WithLogger(log))
}

// Verified wraps next so it only sees authentic requests for skillID.
//
// The body is read once and the signature is checked over exactly those bytes before anything decodes it. Every rejection gets the same 400 so a caller learns nothing about which check failed; the reason is logged. An empty skillID skips the skill check, which is only acceptable before the skill has been deployed.
func Verified(v RequestVerifier, skillID string, log *slog.Logger, next BodyHandler) http.Handler {
	return VerifiedFunc(v, func() string { return skillID }, log, next)
}

// VerifiedFunc is Verified with the skill ID looked up on every request, so a skill deployed while Encore runs is enforced at once.
func VerifiedFunc(v RequestVerifier, skillID func() string, log *slog.Logger, next BodyHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
		if err != nil {
			reject(w, log, r, "reading body", err)

			return
		}

		start := time.Now()

		if err := v.Verify(r.Context(), body, r.Header); err != nil {
			reject(w, log, r, "signature verification", err)

			return
		}

		r = r.WithContext(context.WithValue(r.Context(), verifyTimeKey{}, time.Since(start)))

		if want := skillID(); want != "" {
			got, err := applicationID(body)
			if err != nil {
				reject(w, log, r, "decoding envelope", err)

				return
			}

			if got != want {
				reject(w, log, r, "skill ID", errors.New("request is for application "+got))

				return
			}
		}

		next(w, r, body)
	})
}

// verifyTime returns how long VerifiedFunc took to check the request's signature, or zero for a request it didn't check.
func verifyTime(ctx context.Context) time.Duration {
	d, _ := ctx.Value(verifyTimeKey{}).(time.Duration)

	return d
}

func reject(w http.ResponseWriter, log *slog.Logger, r *http.Request, stage string, err error) {
	log.WarnContext(r.Context(), "rejected Alexa request", "stage", stage, "error", err, "remote", r.RemoteAddr)
	http.Error(w, "bad request", http.StatusBadRequest)
}

// context.System is present on every request type; session is absent on some (e.g. AudioPlayer events), so it is only a fallback.
func applicationID(body []byte) (string, error) {
	var env struct {
		Context struct {
			System struct {
				Application struct {
					ApplicationID string `json:"applicationId"`
				} `json:"application"`
			} `json:"System"`
		} `json:"context"`
		Session struct {
			Application struct {
				ApplicationID string `json:"applicationId"`
			} `json:"application"`
		} `json:"session"`
	}

	if err := json.Unmarshal(body, &env); err != nil {
		return "", err
	}

	if id := env.Context.System.Application.ApplicationID; id != "" {
		return id, nil
	}

	return env.Session.Application.ApplicationID, nil
}
