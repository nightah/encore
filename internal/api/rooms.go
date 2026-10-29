package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/store"
)

func (a *API) registerRooms(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "listRooms", Method: http.MethodGet, Path: Prefix + "/rooms", Tags: []string{"rooms"}, Summary: "List rooms"}),
		func(ctx context.Context, _ *struct{}) (*body[[]Room], error) {
			rooms, err := a.app.DB().Rooms().List(ctx)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			out := make([]Room, 0, len(rooms))

			for _, r := range rooms {
				room, err := a.room(ctx, r)
				if err != nil {
					return nil, a.problemFor(ctx, err)
				}

				out = append(out, room)
			}

			return &body[[]Room]{Body: out}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "createRoom", Method: http.MethodPost, Path: Prefix + "/rooms", Tags: []string{"rooms"}, Summary: "Add a room", DefaultStatus: http.StatusCreated}),
		func(ctx context.Context, in *struct {
			Body struct {
				Name           string `json:"name" minLength:"1" maxLength:"64"`
				KodiInstanceID *int64 `json:"kodi_instance_id,omitempty"`
			}
		},
		) (*body[Room], error) {
			r := store.Room{Name: strings.TrimSpace(in.Body.Name)}
			if in.Body.KodiInstanceID != nil {
				r.KodiInstanceID = *in.Body.KodiInstanceID
			}

			if err := a.app.DB().Rooms().Create(ctx, &r); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			room, err := a.room(ctx, r)

			return &body[Room]{Body: room}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateRoom", Method: http.MethodPatch, Path: Prefix + "/rooms/{id}", Tags: []string{"rooms"}, Summary: "Rename a room or change its Kodi instance"}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Name           *string    `json:"name,omitempty" minLength:"1" maxLength:"64"`
				KodiInstanceID OptionalID `json:"kodi_instance_id,omitempty"`
			}
		},
		) (*body[Room], error) {
			repo := a.app.DB().Rooms()

			r, err := repo.Get(ctx, in.ID)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			if in.Body.Name != nil {
				r.Name = strings.TrimSpace(*in.Body.Name)
			}

			if in.Body.KodiInstanceID.Set {
				r.KodiInstanceID = in.Body.KodiInstanceID.ID
			}

			if err := repo.Update(ctx, &r); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			room, err := a.room(ctx, r)

			return &body[Room]{Body: room}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "deleteRoom", Method: http.MethodDelete, Path: Prefix + "/rooms/{id}", Tags: []string{"rooms"}, Summary: "Remove a room", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, a.problemFor(ctx, a.app.DB().Rooms().Delete(ctx, in.ID))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "addRoomAlias", Method: http.MethodPost, Path: Prefix + "/rooms/{id}/aliases", Tags: []string{"rooms"}, Summary: "Add another spoken name for a room, or for a device in it", DefaultStatus: http.StatusCreated}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Alias string `json:"alias" minLength:"1" maxLength:"64"`
				Kind  string `json:"kind,omitempty" enum:"room,device" default:"room"`
			}
		},
		) (*body[RoomAlias], error) {
			if _, err := a.app.DB().Rooms().Get(ctx, in.ID); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			al, err := a.app.DB().Rooms().AddAlias(ctx, in.ID, strings.TrimSpace(in.Body.Alias), store.SpokenKind(in.Body.Kind))
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			return &body[RoomAlias]{Body: RoomAlias{ID: al.ID, Alias: al.Alias, Kind: string(al.Kind)}}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "deleteRoomAlias", Method: http.MethodDelete, Path: Prefix + "/rooms/{id}/aliases/{alias_id}", Tags: []string{"rooms"}, Summary: "Remove a room alias", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *roomAliasPath) (*struct{}, error) {
			aliases, err := a.app.DB().Rooms().Aliases(ctx, in.ID)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			if !slices.ContainsFunc(aliases, func(al store.RoomAlias) bool { return al.ID == in.AliasID }) {
				return nil, huma.Error404NotFound("not found")
			}

			return nil, a.problemFor(ctx, a.app.DB().Rooms().RemoveAlias(ctx, in.AliasID))
		})
}

func (a *API) room(ctx context.Context, r store.Room) (Room, error) {
	aliases, err := a.app.DB().Rooms().Aliases(ctx, r.ID)
	if err != nil {
		return Room{}, err
	}

	out := Room{ID: r.ID, Name: r.Name, KodiInstanceID: idPtr(r.KodiInstanceID), Aliases: make([]RoomAlias, 0, len(aliases))}
	for _, al := range aliases {
		out.Aliases = append(out.Aliases, RoomAlias{ID: al.ID, Alias: al.Alias, Kind: string(al.Kind)})
	}

	return out, nil
}
