// Package smapitest is an in-memory fake of the parts of SMAPI and Login with Amazon that Encore uses, for tests.
//
// It serves the LWA token endpoint, vendors, skill listing, the skill package flow (upload URL, presigned PUT, asynchronous import), skill status with per-locale build results, development enablement, SSL certificates and simulations. Asynchronous operations report IN_PROGRESS for Options.Pending polls before finishing. Failures are injected with FailLocale, Inject and RevokeRefreshToken.
package smapitest
