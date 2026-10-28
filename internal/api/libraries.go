package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func (a *API) registerLibraries(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "listLibraries", Method: http.MethodGet, Path: Prefix + "/libraries", Tags: []string{"libraries"}, Summary: "List libraries"}),
		func(ctx context.Context, _ *struct{}) (*body[[]Library], error) {
			libs, err := a.libraries.List(ctx)
			if libs == nil {
				libs = []Library{}
			}

			return &body[[]Library]{Body: libs}, a.libraryProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateLibrary", Method: http.MethodPatch, Path: Prefix + "/libraries/{id}", Tags: []string{"libraries"}, Summary: "Choose which instance syncs a shared library"}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				SourceInstanceID int64 `json:"source_instance_id"`
			}
		},
		) (*body[Library], error) {
			lib, err := a.libraries.SetSource(ctx, in.ID, in.Body.SourceInstanceID)
			if err == nil {
				a.events.publish("library", lib)
			}

			return &body[Library]{Body: lib}, a.libraryProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "groupLibrary", Method: http.MethodPost, Path: Prefix + "/libraries/group", Tags: []string{"libraries"}, Summary: "Group Kodi instances that share a library"}),
		func(ctx context.Context, in *struct {
			Body struct {
				InstanceIDs []int64 `json:"instance_ids" minItems:"2"`
			}
		},
		) (*body[Library], error) {
			lib, err := a.libraries.Group(ctx, in.Body.InstanceIDs)
			if err == nil {
				a.events.publish("library", lib)
			}

			return &body[Library]{Body: lib}, a.libraryProblem(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "splitLibrary", Method: http.MethodPost, Path: Prefix + "/libraries/{id}/split", Tags: []string{"libraries"}, Summary: "Take a Kodi instance out of a shared library", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				InstanceID int64 `json:"instance_id"`
			}
		},
		) (*struct{}, error) {
			return nil, a.libraryProblem(ctx, a.libraries.Split(ctx, in.ID, in.Body.InstanceID))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "listRecentTitles", Method: http.MethodGet, Path: Prefix + "/libraries/{id}/recent", Tags: []string{"libraries"}, Summary: "Recently added titles"}),
		func(ctx context.Context, in *struct {
			ID    int64 `path:"id"`
			Limit int   `query:"limit" default:"20" minimum:"1" maximum:"200"`
		},
		) (*body[[]RecentTitle], error) {
			out, err := a.libraries.Recent(ctx, in.ID, in.Limit)
			if out == nil {
				out = []RecentTitle{}
			}

			return &body[[]RecentTitle]{Body: out}, a.libraryProblem(ctx, err)
		})
}

func (a *API) libraryProblem(ctx context.Context, err error) error {
	if errors.Is(err, app.ErrManagedByConfig) {
		return managedError("this library group (kodi[].library_group)")
	}

	return a.problemFor(ctx, err)
}

func (l *appLibraries) List(context.Context) ([]Library, error) {
	groups := l.api.app.Groups()
	out := make([]Library, 0, len(groups))

	for i := range groups {
		out = append(out, toLibrary(&groups[i]))
	}

	return out, nil
}

func toLibrary(g *app.GroupStatus) Library {
	lib := Library{
		ID:                g.ID,
		Name:              g.Name,
		Members:           make([]LibraryMember, 0, len(g.Members)),
		SourceInstanceID:  g.SourceID,
		SyncingInstanceID: g.ActiveID,
		State:             string(g.State),
		Evidence:          g.Evidence,
		LastSyncAt:        timePtr(g.LastSync),
		Counts:            LibraryCounts{Movies: g.Counts[media.KindMovie], Shows: g.Counts[media.KindTVShow], Songs: g.Counts[media.KindSong]},
	}

	if lib.Evidence == "" {
		lib.Evidence = g.Reason
	}

	for _, m := range g.Members {
		lib.Members = append(lib.Members, LibraryMember{InstanceID: m.ID, Name: m.Name})
	}

	return lib
}

func (l *appLibraries) find(match func(*app.GroupStatus) bool) (Library, error) {
	groups := l.api.app.Groups()

	for i := range groups {
		if match(&groups[i]) {
			return toLibrary(&groups[i]), nil
		}
	}

	return Library{}, store.ErrNotFound
}

func (l *appLibraries) Get(_ context.Context, id int64) (Library, error) {
	return l.find(func(g *app.GroupStatus) bool { return g.ID == id })
}

func (l *appLibraries) LibraryOf(_ context.Context, instanceID int64) (Library, error) {
	return l.find(func(g *app.GroupStatus) bool {
		return slices.ContainsFunc(g.Members, func(m app.GroupMember) bool { return m.ID == instanceID })
	})
}

func (l *appLibraries) SetSource(ctx context.Context, id, instanceID int64) (Library, error) {
	if err := l.api.app.SetGroupSource(ctx, id, instanceID); err != nil {
		return Library{}, err
	}

	return l.Get(ctx, id)
}

func (l *appLibraries) Group(ctx context.Context, instanceIDs []int64) (Library, error) {
	id, err := l.api.app.GroupInstances(ctx, "", instanceIDs...)
	if err != nil {
		return Library{}, err
	}

	return l.Get(ctx, id)
}

func (l *appLibraries) Split(ctx context.Context, id, instanceID int64) error {
	lib, err := l.Get(ctx, id)
	if err != nil {
		return err
	}

	if !slices.ContainsFunc(lib.Members, func(m LibraryMember) bool { return m.InstanceID == instanceID }) {
		return fmt.Errorf("kodi instance %d is not in library %d: %w", instanceID, id, store.ErrInvalid)
	}

	return l.api.app.SplitInstance(ctx, instanceID)
}

// listedTitle names an item in a list of titles: an episode's title alone means little, so it follows its show's.
func listedTitle(it media.Item) string {
	if it.Kind == media.KindEpisode && it.ShowTitle != "" {
		return it.ShowTitle + ": " + it.Title
	}

	return it.Title
}

func (l *appLibraries) Recent(ctx context.Context, id int64, limit int) ([]RecentTitle, error) {
	lib, err := l.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Every member reads the group's catalog, so the source answers for all of them.
	src := lib.SourceInstanceID

	items, err := l.api.app.RecentlyAdded(src, limit)
	if err != nil {
		return nil, err
	}

	inModel := l.api.skill.inSkillModel(ctx)
	out := make([]RecentTitle, 0, len(items))

	for _, it := range items {
		out = append(out, RecentTitle{
			Title: listedTitle(it), Year: it.Year, Kind: string(it.Kind), Instance: lib.Name, AddedAt: timePtr(it.DateAdded),
			Findable: l.api.app.Findable(src, it.Ref()), InSkillModel: inModel(src, it),
		})
	}

	return out, nil
}
