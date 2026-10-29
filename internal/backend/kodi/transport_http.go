package kodi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (t *Transport) httpCall(ctx context.Context, id int64, method string, params, result any) error {
	body, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("kodi: %s: encoding params: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.HTTPURL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("kodi: %s: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")

	if t.cfg.Username != "" {
		req.SetBasicAuth(t.cfg.Username, t.cfg.Password)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctxErr(method, ctx.Err())
		}

		return unreachable(method, err)
	}

	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	t.noteAuth(resp.StatusCode)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return unreachable(method, ErrUnauthorized)
	case resp.StatusCode != http.StatusOK:
		return unreachable(method, fmt.Errorf("HTTP %s", resp.Status))
	}

	var msg message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		if ctx.Err() != nil {
			return ctxErr(method, ctx.Err())
		}

		return unreachable(method, fmt.Errorf("decoding response: %w", err))
	}

	return msg.decode(method, result)
}
