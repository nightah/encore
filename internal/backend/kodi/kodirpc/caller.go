package kodirpc

import "context"

// Call invokes method through c and decodes the result as R.
func Call[P, R any](ctx context.Context, c Caller, method string, p P) (R, error) {
	var r R
	err := c.Call(ctx, method, p, &r)
	return r, err
}
