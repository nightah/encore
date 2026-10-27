package kodirpc

import (
	"context"
	"encoding/json"
)

// Caller performs a single JSON-RPC request. params is marshalled as the request's "params" member and the response's "result" member is unmarshalled into result, which is a non-nil pointer.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Client exposes one typed method per Kodi JSON-RPC method on top of a Caller.
type Client struct {
	Caller
}

// APIVersion is a Kodi JSON-RPC API version as reported by JSONRPC.Version.
type APIVersion struct {
	Major, Minor, Patch int
}

// MethodInfo describes a JSON-RPC method of the schema.
type MethodInfo struct {
	Name        string
	Description string
	Permission  string
	Transport   []string
	// Params lists the parameter names in positional order.
	Params []string
	// Since is the oldest pinned API version known to provide the method. Methods present in every pinned schema report BaselineVersion; the method may well be older than that.
	Since APIVersion
	// ParamsSince lists parameters that are newer than the method itself, with the API version that introduced them.
	ParamsSince map[string]APIVersion
}

// Embedded by every generated union type (a schema "type" that lists several alternatives). It holds either a value set through a generated New...From... constructor or the raw JSON received from Kodi. The generated As... accessors don't check that the JSON matches the requested alternative, so callers pick it from context or by inspecting Raw.
type union struct {
	val any
	raw json.RawMessage
}
