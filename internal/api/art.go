package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/backend/kodi"
)

func (a *API) registerArt(api huma.API) {
	op := protected(huma.Operation{
		OperationID: "getKodiArt", Method: http.MethodGet, Path: Prefix + "/kodi/instances/{id}/art", Tags: []string{"kodi"},
		Summary:     "Fetch artwork from a Kodi instance",
		Description: "Fetches an image:// reference (as in now_playing.art_url) from the instance's web server with its credentials, so the browser needn't reach Kodi itself. Anything other than an image:// reference is refused.",
		Responses: map[string]*huma.Response{
			"200": {Description: "The image", Content: map[string]*huma.MediaType{"image/*": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}}},
		},
	})

	huma.Register(api, op, func(ctx context.Context, in *struct {
		ID  int64  `path:"id"`
		Ref string `query:"ref" required:"true" maxLength:"2048" doc:"The image:// reference"`
	},
	) (*huma.StreamResponse, error) {
		body, contentType, err := a.app.InstanceImage(ctx, in.ID, in.Ref)

		switch {
		case errors.Is(err, kodi.ErrNotImage):
			return nil, fieldError("ref", "must be one of Kodi's image:// references", in.Ref)
		case errors.Is(err, app.ErrNoRuntime):
			return nil, a.problemFor(ctx, err)
		case err != nil:
			return nil, huma.Error502BadGateway("Kodi didn't return the image")
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer body.Close()

			hctx.SetHeader("Content-Type", artContentType(contentType))
			hctx.SetHeader("Content-Security-Policy", artCSP)
			hctx.SetHeader("Cache-Control", "private, max-age="+artCacheSeconds)
			hctx.SetHeader("X-Content-Type-Options", "nosniff")
			_, _ = io.Copy(hctx.BodyWriter(), body)
		}}, nil
	})
}

// artContentType passes on Kodi's image types and labels anything else as opaque bytes, so a Kodi instance can't serve a page that runs on Encore's origin.
func artContentType(ct string) string {
	media, _, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(media, "image/") {
		return "application/octet-stream"
	}

	return ct
}
