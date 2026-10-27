// Package smapi is a thin, typed layer over the generated Skill Management API client.
//
// It adds the LWA bearer token to every SMAPI request, retries throttled and failed requests with backoff (honouring Retry-After), polls asynchronous operations, and turns SMAPI's error bodies into *Error. Responses are decoded into this package's types rather than the generated ones because the generated models lose fields (see tools/smapigen/README.md).
package smapi
