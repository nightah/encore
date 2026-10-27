package smapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/smapi/smapiclient"
)

// Problems returns the build's errors, including those reported only on its steps.
func (b BuildStatus) Problems() []Problem {
	out := append([]Problem(nil), b.Errors...)

	if b.BuildDetails != nil {
		for _, s := range b.BuildDetails.Steps {
			for _, p := range s.Errors {
				if !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
	}

	return out
}

// Vendors lists the developer accounts the user belongs to.
func (c *Client) Vendors(ctx context.Context) ([]Vendor, error) {
	call := func() (*http.Response, error) {
		return c.api.GetVendorListV1(ctx)
	}

	var out struct {
		Vendors []Vendor `json:"vendors"`
	}

	if err := decode("list vendors", call, &out); err != nil {
		return nil, err
	}

	return out.Vendors, nil
}

// ListSkills lists every skill in a vendor account, following pagination.
func (c *Client) ListSkills(ctx context.Context, vendorID string) ([]SkillSummary, error) {
	var (
		all  []SkillSummary
		next *string
	)

	pageSize := 50

	for {
		call := func() (*http.Response, error) {
			return c.api.ListSkillsForVendorV1(ctx, &smapiclient.ListSkillsForVendorV1Params{
				VendorID: vendorID, NextToken: next, MaxResults: &pageSize,
			})
		}

		var page struct {
			Skills      []SkillSummary `json:"skills"`
			IsTruncated bool           `json:"isTruncated"`
			NextToken   string         `json:"nextToken"`
		}

		if err := decode("list skills", call, &page); err != nil {
			return nil, err
		}

		all = append(all, page.Skills...)

		if !page.IsTruncated || page.NextToken == "" {
			return all, nil
		}

		next = &page.NextToken
	}
}

// CreateUpload asks for a presigned URL to upload a skill package to.
func (c *Client) CreateUpload(ctx context.Context) (*Upload, error) {
	call := func() (*http.Response, error) {
		return c.api.CreateUploadURLV1(ctx)
	}

	var out Upload
	if err := decode("create upload URL", call, &out); err != nil {
		return nil, err
	}

	if out.URL == "" {
		return nil, errors.New("smapi: create upload URL: response has no uploadUrl")
	}

	return &out, nil
}

// PutPackage uploads a zipped skill package to a presigned URL. The request carries no bearer token: the URL is its own authorisation.
func (c *Client) PutPackage(ctx context.Context, uploadURL string, zip []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(zip))
	if err != nil {
		return fmt.Errorf("smapi: upload package: %w", redactURL(err))
	}

	return do("upload package", func() (*http.Response, error) {
		resp, err := c.upload.Do(req)

		return resp, redactURL(err)
	})
}

// redactURL drops the query from the URL a *url.Error quotes, since a presigned URL's query is its credential.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL, _, _ = strings.Cut(ue.URL, "?")
	}

	return err
}

// ImportNew creates a skill in vendorID from a package uploaded to location and returns the import ID.
func (c *Client) ImportNew(ctx context.Context, vendorID, location string) (string, error) {
	call := func() (*http.Response, error) {
		return c.api.CreateSkillPackageV1(ctx, smapiclient.CreateSkillPackageV1JSONRequestBody{Location: location, VendorID: &vendorID})
	}

	return importID("import new skill", call)
}

// ImportExisting updates skillID's development stage from a package uploaded to location and returns the import ID. A non-empty eTag makes the import fail if the skill changed since.
func (c *Client) ImportExisting(ctx context.Context, skillID, location, eTag string) (string, error) {
	var params smapiclient.ImportSkillPackageV1Params
	if eTag != "" {
		params.IfMatch = &eTag
	}

	call := func() (*http.Response, error) {
		return c.api.ImportSkillPackageV1(ctx, skillID, &params, smapiclient.ImportSkillPackageV1JSONRequestBody{Location: location})
	}

	return importID("import skill package", call)
}

func importID(op string, call caller) (string, error) {
	_, hdr, err := send(op, call)
	if err != nil {
		return "", err
	}

	loc := hdr.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("smapi: %s: response has no Location header", op)
	}

	return path.Base(loc), nil
}

// ImportStatus returns the state of an import.
func (c *Client) ImportStatus(ctx context.Context, importID string) (*ImportStatus, error) {
	call := func() (*http.Response, error) {
		return c.api.GetImportStatusV1(ctx, importID)
	}

	var out ImportStatus
	if err := decode("get import status", call, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// SkillStatus returns the build state of the skill's manifest and every interaction model.
func (c *Client) SkillStatus(ctx context.Context, skillID string) (*SkillStatus, error) {
	call := func() (*http.Response, error) {
		// Unfiltered, the live API reports only the manifest; the per-locale model builds come only when asked for.
		resource := "interactionModel"

		return c.api.GetSkillStatusV1(ctx, skillID, &smapiclient.GetSkillStatusV1Params{Resource: &resource})
	}

	var raw json.RawMessage
	if err := decode("get skill status", call, &raw); err != nil {
		return nil, err
	}

	var out SkillStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("smapi: get skill status: decoding response: %w", err)
	}

	out.Raw = raw

	return &out, nil
}

// EnableDevelopment enables the skill's development stage for testing on the user's devices.
func (c *Client) EnableDevelopment(ctx context.Context, skillID string) error {
	return do("enable skill", func() (*http.Response, error) {
		return c.api.SetSkillEnablementV1(ctx, skillID, StageDevelopment)
	})
}
