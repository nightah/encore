package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/auth"
)

func (a *API) registerReachability(api huma.API) {
	huma.Register(api, public(huma.Operation{
		OperationID: "checkReachability", Method: http.MethodPost, Path: Prefix + "/setup/reachability", Tags: []string{"auth"}, Summary: "Check Encore's public address",
		Description: "Probes the public URL the way Alexa would: an unsigned POST to /api/v1/alexa must be answered 400 by encore; the TLS certificate must be trusted and the endpoint must accept TLS 1.2, the only version Alexa offers; and, for information, whether other /api/v1 routes are reachable through the proxy too (they always need a sign-in). Needs an admin session, or before the admin account exists, the setup token.",
	}),
		func(ctx context.Context, in *struct {
			Body struct {
				URL        string `json:"url" minLength:"1" maxLength:"2048" doc:"The public base URL, e.g. https://encore.example.net"`
				SetupToken string `json:"setup_token,omitempty" doc:"Required before the admin account exists, unless signed in"`
			}
		},
		) (*body[Reachability], error) {
			ri := info(ctx)
			if ri.principal == nil {
				if err := a.auth.CheckSetupToken(ri.r, in.Body.SetupToken); err != nil {
					if errors.Is(err, auth.ErrAdminExists) {
						return nil, huma.Error401Unauthorized("sign in first")
					}

					return nil, a.authProblem(ctx, err)
				}
			}

			base, err := url.Parse(strings.TrimSpace(in.Body.URL))
			if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
				return nil, fieldError("body.url", "must be an http(s) URL such as https://encore.example.net", in.Body.URL)
			}

			base.RawQuery, base.Fragment = "", ""
			base.Path = strings.TrimSuffix(base.Path, "/")

			return &body[Reachability]{Body: a.reachability(ctx, base)}, nil
		})
}

func (a *API) reachability(ctx context.Context, base *url.URL) Reachability {
	out := Reachability{URL: base.String()}

	// Connect where Alexa would: to the address public DNS gives, which split DNS or a proxy such as Cloudflare can make different from what this server resolves.
	route := a.publicRoute(ctx, base.Hostname())

	// Never follow redirects: Alexa does not, and the check must describe this URL.
	client := *a.opts.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = route.transport(a.opts.HTTPClient.Transport)

	alexa, tlsState, alexaErr := probe(ctx, &client, http.MethodPost, base.String()+alexaPath, strings.NewReader("{}"))

	switch {
	case alexaErr != nil:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "alexa_endpoint", Level: "fail", Detail: "could not connect: " + alexaErr.Error()})
	case alexa == http.StatusBadRequest:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "alexa_endpoint", Level: "ok", Detail: "Encore answered and refused an unsigned request, as it should"})
	case alexa == http.StatusServiceUnavailable:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "alexa_endpoint", Level: "warn", Detail: "reached a server that answered 503; Encore may still be starting, or the proxy has no backend"})
	default:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "alexa_endpoint", Level: "fail", Detail: fmt.Sprintf("expected Encore's 400 for an unsigned request but got %d; check the proxy forwards %s to Encore", alexa, alexaPath)})
	}

	route.annotate(&out.Checks[len(out.Checks)-1])

	cert := a.certificateCheck(base, tlsState, alexaErr)
	if cert.Level == "ok" || cert.Level == "warn" {
		a.tls12Check(ctx, &cert, base, route)
	}

	out.Checks = append(out.Checks, cert)

	settings, _, err := probe(ctx, &client, http.MethodGet, base.String()+Prefix+"/settings", nil)

	switch {
	case err != nil, settings == http.StatusNotFound, settings == http.StatusForbidden:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "admin_exposed", Level: "ok", Detail: "only the Alexa endpoint is reachable from outside"})
	case settings == http.StatusUnauthorized:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "admin_exposed", Level: "ok", Detail: "the web interface and admin API are reachable from outside too", Note: "Every admin route needs a sign-in."})
	default:
		out.Checks = append(out.Checks, ReachabilityCheck{Name: "admin_exposed", Level: "ok", Detail: fmt.Sprintf("other routes answered %d, not the admin API", settings)})
	}

	return out
}

func (a *API) certificateCheck(base *url.URL, state *tls.ConnectionState, err error) ReachabilityCheck {
	c := ReachabilityCheck{Name: "certificate"}

	var (
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
	)

	switch {
	case base.Scheme != "https":
		c.Level, c.Detail = "fail", "Alexa only calls HTTPS endpoints; serve Encore through a proxy with a certificate"
	case errors.As(err, &unknown):
		c.Level, c.Detail = "fail", "the certificate is not signed by a public certificate authority, which Alexa requires; use one such as Let's Encrypt"
	case errors.As(err, &hostname):
		c.Level, c.Detail = "fail", "the certificate does not cover "+base.Hostname()
	case errors.As(err, &invalid):
		c.Level, c.Detail = "fail", "the certificate is invalid: "+invalid.Error()
	case state == nil || len(state.PeerCertificates) == 0:
		c.Level, c.Detail = "warn", "could not read the certificate"
	default:
		leaf := state.PeerCertificates[0]
		issuer := leaf.Issuer.CommonName

		if len(leaf.Issuer.Organization) > 0 {
			issuer = leaf.Issuer.Organization[0] + " (" + leaf.Issuer.CommonName + ")"
		}

		left := leaf.NotAfter.Sub(a.now())
		c.Level, c.Detail = "ok", fmt.Sprintf("issued by %s, expires %s", issuer, leaf.NotAfter.UTC().Format(time.DateOnly))

		if left < certWarnWithin {
			c.Level = "warn"
			c.Detail += fmt.Sprintf(" (in %d days)", int(left.Hours()/24))
		}
	}

	return c
}

// probe makes one request and returns its status and TLS state. The body is never read beyond what closing needs.
func probe(ctx context.Context, client *http.Client, method, target string, body *strings.Reader) (int, *tls.ConnectionState, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req *http.Request

	var err error

	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, target, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, target, http.NoBody)
	}

	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// An admin, or the holder of the setup token, asks Encore to probe its own public URL.
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}

	_ = resp.Body.Close()

	return resp.StatusCode, resp.TLS, nil
}
