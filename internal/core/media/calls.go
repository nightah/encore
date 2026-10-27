package media

import (
	"context"
)

// ObserveCalls returns a context under which backends report each call they make to fn, so a voice request can log what it asked Kodi to do. fn may be called from several goroutines at once.
func ObserveCalls(ctx context.Context, fn func(Call)) context.Context {
	return context.WithValue(ctx, callObserverKey{}, fn)
}

// ReportCall tells the observer set by ObserveCalls, if any, about a call. Backends call it once per request to the media centre.
func ReportCall(ctx context.Context, c Call) {
	if fn, ok := ctx.Value(callObserverKey{}).(func(Call)); ok {
		fn(c)
	}
}
