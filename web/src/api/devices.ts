import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { deleteDevice, listDevices, updateDevice } from "@api/generated/endpoints/devices/devices";
import {
  addRoomAlias,
  createRoom,
  deleteRoom,
  deleteRoomAlias,
  listRooms,
  updateRoom,
} from "@api/generated/endpoints/rooms/rooms";
import type {
  CreateRoomRequest,
  Device,
  Room,
  RoomAliasKind,
  UpdateDeviceRequest,
  UpdateRoomRequest,
} from "@api/generated/model";
import { keys } from "@api/keys";
import i18n from "@i18n/index";

const devicesQuery = queryOptions({
  queryFn: ({ signal }) => listDevices({ signal }),
  queryKey: keys.devices,
});

export function useDevices() {
  return useQuery(devicesQuery);
}

export function useUpdateDevice(success = i18n.t("toast.echoSaved")) {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { inline: true, success },
    mutationFn: ({ id, body }: { id: number; body: UpdateDeviceRequest }) => updateDevice(id, body),
    onSuccess: (device) => {
      queryClient.setQueryData<Device[]>(keys.devices, (list) =>
        list?.map((item) => (item.id === device.id ? device : item)),
      );
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.devices }),
        queryClient.invalidateQueries({ queryKey: keys.overview }),
      ]);
    },
  });
}

export function useDeleteDevice() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.echoForgotten") },
    mutationFn: (id: number) => deleteDevice(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.devices }),
  });
}

const roomsQuery = queryOptions({
  queryFn: ({ signal }) => listRooms({ signal }),
  queryKey: keys.rooms,
});

export function useRooms() {
  return useQuery(roomsQuery);
}

function useInvalidateRooms() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: keys.rooms });
}

export function useCreateRoom() {
  const invalidate = useInvalidateRooms();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.roomAdded") },
    mutationFn: (body: CreateRoomRequest) => createRoom(body),
    onSuccess: invalidate,
  });
}

export function useUpdateRoom() {
  const invalidate = useInvalidateRooms();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.roomSaved") },
    mutationFn: ({ id, body }: { id: number; body: UpdateRoomRequest }) => updateRoom(id, body),
    onSuccess: invalidate,
  });
}

export function useDeleteRoom() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.roomRemoved") },
    mutationFn: (id: number) => deleteRoom(id),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.rooms }),
        queryClient.invalidateQueries({ queryKey: keys.devices }),
      ]),
  });
}

export function useAddRoomAlias() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { inline: true },
    mutationFn: ({ roomId, alias, kind }: { roomId: number; alias: string; kind: RoomAliasKind }) =>
      addRoomAlias(roomId, { alias, kind }),
    onSuccess: (alias, { roomId }) => {
      queryClient.setQueryData<Room[]>(keys.rooms, (list) =>
        list?.map((room) => (room.id === roomId ? { ...room, aliases: [...room.aliases, alias] } : room)),
      );
    },
  });
}

export function useDeleteRoomAlias() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ roomId, aliasId }: { roomId: number; aliasId: number }) => deleteRoomAlias(roomId, aliasId),
    onSuccess: (_data, { roomId, aliasId }) => {
      queryClient.setQueryData<Room[]>(keys.rooms, (list) =>
        list?.map((room) =>
          room.id === roomId ? { ...room, aliases: room.aliases.filter((a) => a.id !== aliasId) } : room,
        ),
      );
    },
  });
}
