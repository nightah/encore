package alexa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jamestelfer/billy/pkg/alexaverify"
)

// NewVerifier returns the production verifier, which trusts the host's root certificates and has no way to be disabled.
func NewVerifier(log *slog.Logger) (*Verifier, error) {
	return newVerifier(alexaverify.WithLogger(log))
}

// newVerifier builds a Verifier with opts. Its check as sent uses the same options, but reads its clock from the request it checks.
func newVerifier(opts ...alexaverify.Option) (*Verifier, error) {
	out := &Verifier{}

	var err error
	if out.Verifier, err = alexaverify.New(opts...); err == nil {
		out.sent, err = alexaverify.New(append(slices.Clone(opts), alexaverify.WithClock(func() time.Time { return out.at }))...)
	}

	return out, err
}

// VerifyAsSent verifies a request as of the timestamp it carries rather than now, which tells a request Alexa signed from one anybody could send when Verify refused it for its timestamp. Checks run one at a time.
func (v *Verifier) VerifyAsSent(ctx context.Context, body []byte, hdr http.Header) error {
	at, err := sentAt(body)
	if err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.at = at

	return v.sent.Verify(ctx, body, hdr)
}

// sentAt is the timestamp a request envelope carries.
func sentAt(body []byte) (time.Time, error) {
	var env struct {
		Request struct {
			Timestamp string `json:"timestamp"`
		} `json:"request"`
	}

	if err := json.Unmarshal(body, &env); err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errNoTimestamp, err)
	}

	at, err := time.Parse(time.RFC3339, env.Request.Timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errNoTimestamp, err)
	}

	return at, nil
}

// Verified wraps next so it only sees authentic requests for skillID.
//
// The body is read once and the signature is checked over exactly those bytes before anything decodes it. Every rejection gets the same 400 so a caller learns nothing about which check failed; the reason is logged. An empty skillID skips the skill check, which is only acceptable before the skill has been deployed.
func Verified(v RequestVerifier, skillID string, log *slog.Logger, next BodyHandler) http.Handler {
	return VerifiedFunc(v, func() string { return skillID }, log, nil, next)
}

// VerifiedFunc is Verified with the skill ID looked up on every request, so a skill deployed while Encore runs is enforced at once, and each rejection counted in rejected unless it is nil.
func VerifiedFunc(v RequestVerifier, skillID func() string, log *slog.Logger, rejected *Rejections, next BodyHandler) http.Handler {
	reject := func(w http.ResponseWriter, r *http.Request, stage RejectStage, step string, err error) {
		rejected.Record(stage)
		log.WarnContext(r.Context(), "rejected Alexa request", "stage", step, "error", err, "remote", r.RemoteAddr)
		http.Error(w, "bad request", http.StatusBadRequest)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
		if err != nil {
			reject(w, r, RejectBody, "reading body", err)

			return
		}

		start := time.Now()

		if err := v.Verify(r.Context(), body, r.Header); err != nil {
			reject(w, r, rejectStage(r.Context(), v, body, r.Header, err), "signature verification", err)

			return
		}

		r = r.WithContext(context.WithValue(r.Context(), verifyTimeKey{}, time.Since(start)))

		if want := skillID(); want != "" {
			got, err := applicationID(body)
			if err != nil {
				reject(w, r, RejectBody, "decoding envelope", err)

				return
			}

			if got != want {
				reject(w, r, RejectSkillID, "skill ID", errors.New("request is for application "+got))

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

// rejectStage sorts err, v's refusal of a request, into the check that refused it. A request refused for its timestamp is only put down to the clock when Alexa signed it as of the time it carries: anybody can send an old timestamp, or none.
func rejectStage(ctx context.Context, v RequestVerifier, body []byte, hdr http.Header, err error) RejectStage {
	stage := verifyStage(err)
	if stage != RejectTimestamp {
		return stage
	}

	sv, ok := v.(sentVerifier)
	if !ok {
		return RejectSignature
	}

	switch err := sv.VerifyAsSent(ctx, body, hdr); {
	case err == nil:
		return RejectTimestamp
	case errors.Is(err, errNoTimestamp):
		return RejectBody
	case errors.Is(err, errUnconfirmed):
		return RejectSignature
	default:
		return verifyStage(err)
	}
}

// verifyStage sorts a verification failure into the check that refused it. A certificate chain URL that isn't Amazon's counts as no signature, since Alexa always sends its own.
func verifyStage(err error) RejectStage {
	switch {
	case errors.Is(err, alexaverify.ErrMissingHeader), errors.Is(err, alexaverify.ErrCertURLInvalid), errors.Is(err, alexaverify.ErrCertURLUserinfo), errors.Is(err, alexaverify.ErrCertURLTraversal):
		return RejectUnsigned
	case errors.Is(err, alexaverify.ErrStaleTimestamp):
		return RejectTimestamp
	default:
		return RejectSignature
	}
}

// NewRejections returns empty counts read with clock now, or time.Now when nil.
func NewRejections(now func() time.Time) *Rejections {
	if now == nil {
		now = time.Now
	}

	return &Rejections{now: now, counts: map[RejectStage]*Rejection{}, hours: map[RejectStage][]rejectedHour{}}
}

// Record counts a rejection at stage. A nil Rejections counts nothing.
func (r *Rejections) Record(stage RejectStage) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	c := r.counts[stage]
	if c == nil {
		c = &Rejection{Stage: stage}
		r.counts[stage] = c
	}

	now := r.now()
	c.Count++
	c.Last = now

	hour := now.Truncate(time.Hour)
	hours := r.hours[stage]

	if n := len(hours); n > 0 && hours[n-1].start.Equal(hour) {
		hours[n-1].n++
	} else {
		hours = append(hours, rejectedHour{start: hour, n: 1})
	}

	// An hour that ended a window ago no longer counts.
	for len(hours) > 0 && !recentHour(hours[0], now) {
		hours = hours[1:]
	}

	r.hours[stage] = hours
}

// Counts returns the stages that have rejected a request since Encore started, in the order the checks run, with how many they rejected in the RecentRejections before now.
func (r *Rejections) Counts(now time.Time) []Rejection {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := []Rejection{}

	for _, stage := range rejectStages {
		c := r.counts[stage]
		if c == nil {
			continue
		}

		rej := *c

		for _, h := range r.hours[stage] {
			if recentHour(h, now) {
				rej.Recent += h.n
			}
		}

		out = append(out, rej)
	}

	return out
}

// recentHour reports whether any of h is in the RecentRejections before now.
func recentHour(h rejectedHour, now time.Time) bool {
	return h.start.Add(time.Hour).After(now.Add(-RecentRejections))
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

// WarmVerifier fills v's certificate cache with the chain at url, the one a previous run verified requests with, so the first request after a start doesn't wait on the download. It is best effort: a failure is logged and the first request fetches the chain itself.
func WarmVerifier(ctx context.Context, v *alexaverify.Verifier, url string, log *slog.Logger) {
	if url == "" {
		return
	}

	if err := v.Warm(ctx, url); err != nil {
		log.WarnContext(ctx, "couldn't warm the Alexa certificate cache", "url", url, "error", err)
	}
}

// RememberCertURL wraps v so seen learns the certificate chain URL of each request v verifies, for WarmVerifier to use after a restart. last is the URL already stored; seen is only called when a verified request brings a different one.
func RememberCertURL(v RequestVerifier, last string, seen func(ctx context.Context, url string)) RequestVerifier {
	return &certURLVerifier{RequestVerifier: v, seen: seen, last: last}
}

func (v *certURLVerifier) Verify(ctx context.Context, body []byte, hdr http.Header) error {
	if err := v.RequestVerifier.Verify(ctx, body, hdr); err != nil {
		return err
	}

	url := hdr.Get(certChainURLHeader)

	v.mu.Lock()
	changed := url != "" && url != v.last
	if changed {
		v.last = url
	}
	v.mu.Unlock()

	if changed {
		v.seen(ctx, url)
	}

	return nil
}

// VerifyAsSent is the wrapped verifier's VerifyAsSent, when it has one. It learns nothing: a request it passes was still refused.
func (v *certURLVerifier) VerifyAsSent(ctx context.Context, body []byte, hdr http.Header) error {
	sv, ok := v.RequestVerifier.(sentVerifier)
	if !ok {
		return errUnconfirmed
	}

	return sv.VerifyAsSent(ctx, body, hdr)
}
