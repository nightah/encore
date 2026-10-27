package smapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrPollTimeout means an asynchronous operation did not finish within PollConfig.Timeout.
var ErrPollTimeout = errors.New("smapi: timed out waiting for the operation to finish")

// Error is a non-success SMAPI response.
type Error struct {
	// Op names the operation, e.g. "create upload URL".
	Op         string
	StatusCode int
	Code       string
	Message    string
	Violations []Problem
}

func (e *Error) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "smapi: %s: HTTP %d", e.Op, e.StatusCode)

	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}

	for i, v := range e.Violations {
		if i == 0 {
			b.WriteString(" (")
		} else {
			b.WriteString("; ")
		}

		b.WriteString(v.String())

		if i == len(e.Violations)-1 {
			b.WriteString(")")
		}
	}

	return b.String()
}

func (p Problem) String() string {
	if p.Code == "" {
		return p.Message
	}

	return p.Code + ": " + p.Message
}

// StatusCode returns the HTTP status of an *Error in err's chain, or 0.
func StatusCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.StatusCode
	}

	return 0
}

func send(op string, call caller) ([]byte, http.Header, error) {
	resp, err := call()
	if err != nil {
		return nil, nil, fmt.Errorf("smapi: %s: %w", op, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("smapi: %s: reading response: %w", op, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, resp.Header, nil
	}

	e := &Error{Op: op, StatusCode: resp.StatusCode}

	var eb struct {
		Code       string    `json:"code"`
		Message    string    `json:"message"`
		Violations []Problem `json:"violations"`
	}

	if json.Unmarshal(body, &eb) == nil {
		e.Code, e.Message, e.Violations = eb.Code, eb.Message, eb.Violations
	}

	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}

	return nil, nil, e
}

func do(op string, call caller) error {
	_, _, err := send(op, call)

	return err
}

func decode(op string, call caller, v any) error {
	body, _, err := send(op, call)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("smapi: %s: decoding response: %w", op, err)
	}

	return nil
}
