package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/store"
)

// UnmarshalJSON implements json.Unmarshaler.
func (o *OptionalID) UnmarshalJSON(b []byte) error {
	o.Set = true

	if string(b) == "null" {
		o.ID = 0

		return nil
	}

	return json.Unmarshal(b, &o.ID)
}

// Schema implements huma.SchemaProvider.
func (OptionalID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeInteger, Format: "int64", Nullable: true}
}

func (a *API) registerDevices(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "listDevices", Method: http.MethodGet, Path: Prefix + "/devices", Tags: []string{"devices"}, Summary: "List Echo devices"}),
		func(ctx context.Context, _ *struct{}) (*body[[]Device], error) {
			out, err := a.devices(ctx)

			return &body[[]Device]{Body: out}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateDevice", Method: http.MethodPatch, Path: Prefix + "/devices/{id}", Tags: []string{"devices"}, Summary: "Name an Echo device, put it in a room or link it to a Kodi instance"}),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Name           *string    `json:"name,omitempty" maxLength:"64"`
				RoomID         OptionalID `json:"room_id,omitempty"`
				KodiInstanceID OptionalID `json:"kodi_instance_id,omitempty"`
			}
		},
		) (*body[Device], error) {
			repo := a.app.DB().EchoDevices()

			d, err := repo.Get(ctx, in.ID)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			if in.Body.Name != nil {
				d.DisplayName = strings.TrimSpace(*in.Body.Name)
			}

			if in.Body.RoomID.Set {
				d.RoomID = in.Body.RoomID.ID
			}

			if in.Body.KodiInstanceID.Set {
				d.KodiInstanceID = in.Body.KodiInstanceID.ID
			}

			if err := repo.Update(ctx, &d); err != nil {
				return nil, a.problemFor(ctx, err)
			}

			dev, err := a.device(ctx, d.ID)
			if err == nil {
				a.events.publish("device", dev)
			}

			return &body[Device]{Body: dev}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "deleteDevice", Method: http.MethodDelete, Path: Prefix + "/devices/{id}", Tags: []string{"devices"}, Summary: "Forget an Echo device", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, in *idPath) (*struct{}, error) {
			return nil, a.problemFor(ctx, a.app.DB().EchoDevices().Delete(ctx, in.ID))
		})
}

func (a *API) devices(ctx context.Context) ([]Device, error) {
	rows, err := a.app.DB().EchoDevices().List(ctx)
	if err != nil {
		return nil, err
	}

	today, err := a.requestsToday(ctx)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	for _, e := range today {
		counts[e.DeviceID]++
	}

	out := make([]Device, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDevice(d, counts[d.AlexaDeviceID]))
	}

	return out, nil
}

func (a *API) device(ctx context.Context, id int64) (Device, error) {
	d, err := a.app.DB().EchoDevices().Get(ctx, id)
	if err != nil {
		return Device{}, err
	}

	today, err := a.requestsToday(ctx)
	if err != nil {
		return Device{}, err
	}

	n := 0

	for _, e := range today {
		if e.DeviceID == d.AlexaDeviceID {
			n++
		}
	}

	return toDevice(d, n), nil
}

func toDevice(d store.EchoDevice, today int) Device {
	return Device{
		ID: d.ID, AlexaDeviceID: d.AlexaDeviceID, Name: d.DisplayName,
		RoomID: idPtr(d.RoomID), KodiInstanceID: idPtr(d.KodiInstanceID),
		FirstSeen: d.FirstSeen, LastSeen: d.LastSeen, RequestCount: d.RequestCount, RequestCountToday: today,
	}
}

func idPtr(id int64) *int64 {
	if id == 0 {
		return nil
	}

	return &id
}
