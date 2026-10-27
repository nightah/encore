package smapi

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (p PollConfig) withDefaults() PollConfig {
	if p.Interval <= 0 {
		p.Interval = 5 * time.Second
	}

	if p.MaxInterval < p.Interval {
		p.MaxInterval = max(15*time.Second, p.Interval)
	}

	if p.Timeout <= 0 {
		p.Timeout = 15 * time.Minute
	}

	return p
}

// Poll calls check until it reports done, returns an error, ctx ends or the timeout passes. The first check is immediate.
func Poll(ctx context.Context, cfg PollConfig, check func(ctx context.Context) (done bool, err error)) error {
	cfg = cfg.withDefaults()

	ctx, cancel := context.WithTimeoutCause(ctx, cfg.Timeout, ErrPollTimeout)
	defer cancel()

	wait := cfg.Interval

	for {
		done, err := check(ctx)
		if err != nil {
			return pollErr(ctx, err)
		}

		if done {
			return nil
		}

		if err := sleep(ctx, wait); err != nil {
			return pollErr(ctx, err)
		}

		wait = min(wait+wait/2, cfg.MaxInterval)
	}
}

func pollErr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrPollTimeout) {
		return fmt.Errorf("%w: %w", ErrPollTimeout, err)
	}

	return err
}

// Poll polls with the client's configuration.
func (c *Client) Poll(ctx context.Context, check func(ctx context.Context) (done bool, err error)) error {
	return Poll(ctx, c.poll, check)
}
