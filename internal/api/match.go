package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func (a *API) registerMatch(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "testMatch", Method: http.MethodPost, Path: Prefix + "/match", Tags: []string{"match"}, Summary: "Test how a spoken title is matched"}),
		func(ctx context.Context, in *struct {
			Body struct {
				LibraryID int64    `json:"library_id"`
				Query     string   `json:"query" minLength:"1" maxLength:"500"`
				Kinds     []string `json:"kinds,omitempty" enum:"movie,tvshow,episode,artist,album,song,musicvideo,playlist"`
			}
		},
		) (*body[MatchResult], error) {
			lib, err := a.libraries.Get(ctx, in.Body.LibraryID)
			if err != nil {
				return nil, a.libraryProblem(ctx, err)
			}

			q := match.Query{Text: in.Body.Query, Limit: 10}
			for _, k := range in.Body.Kinds {
				q.Kinds = append(q.Kinds, media.Kind(k))
			}

			res, err := a.app.Search(ctx, lib.SourceInstanceID, q)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			m := matchResult(res, "", nil)

			return &body[MatchResult]{Body: MatchResult{Normalized: m.Normalized, Decision: m.Decision, Margin: m.Margin, Candidates: m.Candidates}}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "listAliases", Method: http.MethodGet, Path: Prefix + "/aliases", Tags: []string{"match"}, Summary: "List title aliases of a library"}),
		func(ctx context.Context, in *struct {
			LibraryID int64 `query:"library_id" required:"true"`
		},
		) (*body[[]TitleAlias], error) {
			lib, err := a.libraries.Get(ctx, in.LibraryID)
			if err != nil {
				return nil, a.libraryProblem(ctx, err)
			}

			// Aliases are saved against the source at the time, which may since have changed; every member's belong to the library.
			out := []TitleAlias{}

			for _, m := range lib.Members {
				rows, err := a.app.DB().Aliases().List(ctx, m.InstanceID)
				if err != nil {
					return nil, a.problemFor(ctx, err)
				}

				for _, r := range rows {
					out = append(out, a.toAlias(lib.ID, r))
				}
			}

			return &body[[]TitleAlias]{Body: out}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "createAlias", Method: http.MethodPost, Path: Prefix + "/aliases", Tags: []string{"match"}, Summary: "Add a spoken alias for a title", Description: "The alias is added to the skill model on the next deploy.", DefaultStatus: http.StatusCreated}),
		func(ctx context.Context, in *struct {
			Body struct {
				LibraryID int64  `json:"library_id"`
				Kind      string `json:"kind" enum:"movie,tvshow,episode,artist,album,song,musicvideo,playlist"`
				MediaID   int    `json:"media_id" minimum:"1"`
				Alias     string `json:"alias" minLength:"1" maxLength:"140"`
			}
		},
		) (*body[TitleAlias], error) {
			lib, err := a.libraries.Get(ctx, in.Body.LibraryID)
			if err != nil {
				return nil, a.libraryProblem(ctx, err)
			}

			ref := media.Ref{Kind: media.Kind(in.Body.Kind), ID: in.Body.MediaID}
			if _, ok := a.app.Item(lib.SourceInstanceID, ref); !ok {
				return nil, fieldError("body.media_id", "no such title in this library", in.Body.MediaID)
			}

			row := store.TitleAlias{KodiInstanceID: lib.SourceInstanceID, Media: ref, Alias: strings.TrimSpace(in.Body.Alias)}
			if store.Normalise(row.Alias) == "" {
				return nil, fieldError("body.alias", "must contain a letter or digit", in.Body.Alias)
			}

			if err := a.app.DB().Aliases().Add(ctx, &row); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			a.reloadAliases(ctx)

			return &body[TitleAlias]{Body: a.toAlias(lib.ID, row)}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "deleteAlias", Method: http.MethodDelete, Path: Prefix + "/aliases/{id}", Tags: []string{"match"}, Summary: "Remove a title alias", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			if err := a.app.DB().Aliases().Delete(ctx, in.ID); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			a.reloadAliases(ctx)

			return nil, nil
		})
}

// reloadAliases brings the title indexes up to date with a saved alias change. The change is stored either way, so a failure only delays it until the next restart.
func (a *API) reloadAliases(ctx context.Context) {
	if err := a.app.ReloadAliases(ctx); err != nil {
		a.log.Warn("updating title indexes with aliases", "error", err)
	}
}

func (a *API) toAlias(libraryID int64, r store.TitleAlias) TitleAlias {
	out := TitleAlias{ID: r.ID, LibraryID: libraryID, Kind: string(r.Media.Kind), MediaID: r.Media.ID, Alias: r.Alias}

	if it, ok := a.app.Item(r.KodiInstanceID, r.Media); ok {
		out.Title = it.Title
	}

	return out
}
