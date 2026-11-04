package smapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/smapi/smapiclient"
)

// New returns a Client authorising requests with tokens from ts. If ts also has a TokenContext(ctx) method (as *lwa.TokenSource does), the request's context is passed to it.
func New(ts oauth2.TokenSource, opts Options) (*Client, error) {
	if ts == nil {
		return nil, errors.New("smapi: a token source is required")
	}

	if opts.BaseURL == "" {
		opts.BaseURL = BaseURL
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}

	retry := opts.Retry.withDefaults()
	counts := &retryCounts{}

	// NewClient fails only when an option does, and WithHTTPClient never does.
	api, _ := smapiclient.NewClient(opts.BaseURL, smapiclient.WithHTTPClient(&doer{http: hc, tokens: ts, retry: retry, counts: counts}))

	return &Client{
		api:    api,
		upload: &doer{http: hc, retry: retry, counts: counts},
		poll:   opts.Poll.withDefaults(),
		counts: counts,
	}, nil
}

// Retries counts the requests the client has retried so far, and those it gave up on.
func (c *Client) Retries() RetryCounts {
	return RetryCounts{Throttled: c.counts.throttled.Load(), Failed: c.counts.failed.Load(), GaveUp: c.counts.gaveUp.Load()}
}

// API exposes the generated client for operations this package does not wrap. Requests made through it are authorised and retried too.
func (c *Client) API() *smapiclient.Client {
	return c.api
}

func (d *doer) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	refreshed := false
	sent := ""

	for attempt := 1; ; attempt++ {
		if attempt > 1 && hasBody(req) {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("smapi: rewinding request body: %w", err)
			}

			req.Body = body
		}

		if d.tokens != nil {
			tok, err := d.token(ctx)
			if err != nil {
				return nil, err
			}

			sent = tok.AccessToken
			req.Header.Set("Authorization", "Bearer "+sent)
		}

		resp, err := d.http.Do(req)
		if err != nil {
			wait, retry := d.retry.decide(req, 0, nil, err, attempt)
			d.count(0, retry, attempt)

			if !retry {
				return nil, err
			}

			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}

			continue
		}

		// Amazon can stop accepting an access token before its expiry; a fresh one is worth one more try.
		if resp.StatusCode == http.StatusUnauthorized && !refreshed && rewindable(req) && d.rejectToken(sent) {
			refreshed = true

			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
			_ = resp.Body.Close()

			continue
		}

		wait, retry := d.retry.decide(req, resp.StatusCode, resp.Header, nil, attempt)
		if retriable(resp.StatusCode) {
			d.count(resp.StatusCode, retry, attempt)
		}

		if !retry {
			return resp, nil
		}

		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()

		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// count records a throttled or failed attempt (status 0 for a network error): retried, or, after the policy's last attempt, given up on.
func (d *doer) count(status int, retried bool, attempt int) {
	switch {
	case d.counts == nil:
	case !retried:
		if attempt >= d.retry.MaxAttempts {
			d.counts.gaveUp.Add(1)
		}
	case status == http.StatusTooManyRequests:
		d.counts.throttled.Add(1)
	default:
		d.counts.failed.Add(1)
	}
}

// rejectToken makes the next token a new one, unless another request already replaced the rejected one, when the token source can.
func (d *doer) rejectToken(accessToken string) bool {
	r, ok := d.tokens.(rejectingTokenSource)
	if ok {
		r.Rejected(accessToken)
	}

	return ok
}

func (d *doer) token(ctx context.Context) (*oauth2.Token, error) {
	var (
		tok *oauth2.Token
		err error
	)

	if cts, ok := d.tokens.(contextTokenSource); ok {
		tok, err = cts.TokenContext(ctx)
	} else {
		tok, err = d.tokens.Token()
	}

	if err != nil {
		return nil, fmt.Errorf("smapi: getting access token: %w", err)
	}

	return tok, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
