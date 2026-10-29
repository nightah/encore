import { useState } from "react";

import { Pencil, X } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import { useAddRoomAlias, useCreateRoom, useDeleteRoom, useDeleteRoomAlias, useUpdateRoom } from "@api/devices";
import { type Instance, type Room, RoomAliasKind } from "@api/generated/model";
import { KindPill } from "@components/KindPill";
import { Button, IconButton } from "@components/ui/Button";
import { cn, focusRing } from "@components/ui/cn";
import { Dialog } from "@components/ui/Dialog";
import { Field, Input, Select } from "@components/ui/Field";

/** Rooms and their spoken names, used for "play Bluey in the kids' room". */
export function RoomsSection({ rooms, instances }: { rooms: Room[]; instances: Instance[] }) {
  const { t } = useTranslation("devices");
  const [dialog, setDialog] = useState<{ room?: Room } | null>(null);
  return (
    <section
      aria-labelledby="rooms-h"
      className="flex flex-col gap-3.5 rounded-xl border border-line bg-surface px-4.5 py-4"
    >
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <h2 id="rooms-h" className="font-display font-semibold text-lg">
          {t("rooms.title")}
        </h2>
        <Button onClick={() => setDialog({})}>{t("rooms.add")}</Button>
      </div>
      <p className="text-muted">
        <Trans
          t={t}
          i18nKey="rooms.intro"
          components={{
            b: <b />,
            device: <KindPill kind={RoomAliasKind.device} className="pe-2.5 align-middle" />,
            room: <KindPill kind={RoomAliasKind.room} className="pe-2.5 align-middle" />,
          }}
        />
      </p>
      {rooms.length === 0 ? <p className="text-muted">{t("rooms.empty")}</p> : null}
      <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        {rooms.map((room) => (
          <RoomCard
            key={room.id}
            room={room}
            instance={instances.find((i) => i.id === room.kodi_instance_id)}
            onEdit={() => setDialog({ room })}
          />
        ))}
      </div>
      <RoomDialog open={dialog !== null} room={dialog?.room} instances={instances} onClose={() => setDialog(null)} />
    </section>
  );
}

function RoomCard({ room, instance, onEdit }: { room: Room; instance: Instance | undefined; onEdit: () => void }) {
  const { t } = useTranslation("devices");
  const add = useAddRoomAlias();
  const remove = useDeleteRoomAlias();
  const [alias, setAlias] = useState("");
  const [kind, setKind] = useState<RoomAliasKind>(RoomAliasKind.room);

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-line p-3.5">
      <div className="flex items-start justify-between gap-2">
        <span className="font-semibold">
          {room.name}{" "}
          <span className="font-normal text-muted">
            {instance ? t("rooms.card.kodi", { name: instance.name }) : t("rooms.card.noKodi")}
          </span>
        </span>
        <IconButton
          label={t("rooms.card.edit", { name: room.name })}
          variant="ghost"
          className="-mt-2 -me-2"
          onClick={onEdit}
        >
          <Pencil aria-hidden="true" className="size-4" />
        </IconButton>
      </div>
      <ul aria-label={t("rooms.card.aliases", { name: room.name })} className="flex flex-wrap gap-1.5">
        {room.aliases.map((a) => (
          <li key={a.id}>
            <KindPill kind={a.kind} className="pe-1">
              <span className="sr-only">{t(`rooms.kind.${a.kind}`)}: </span>
              <span>{a.alias}</span>
              <button
                type="button"
                aria-label={t("rooms.card.removeAlias", { alias: a.alias })}
                onClick={() => remove.mutate({ aliasId: a.id, roomId: room.id })}
                className={cn(
                  "relative inline-flex size-5 items-center justify-center rounded-full hover:bg-black/10 after:absolute after:-inset-3 after:content-['']",
                  focusRing,
                )}
              >
                <X aria-hidden="true" className="size-3" />
              </button>
            </KindPill>
          </li>
        ))}
      </ul>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!alias.trim()) return;
          add.mutate({ alias: alias.trim(), kind, roomId: room.id }, { onSuccess: () => setAlias("") });
        }}
      >
        <Field
          label={t("rooms.card.addAliasLabel", { name: room.name })}
          hideLabel
          className="basis-full"
          error={add.error ? errorMessage(add.error) : undefined}
        >
          <Input
            placeholder={t(`rooms.card.placeholder.${kind}`)}
            value={alias}
            onChange={(event) => {
              setAlias(event.target.value);
              if (add.error) add.reset();
            }}
          />
        </Field>
        <Field label={t("rooms.card.kindLabel", { name: room.name })} hideLabel className="flex-1">
          <Select value={kind} onChange={(event) => setKind(event.target.value as RoomAliasKind)}>
            <option value={RoomAliasKind.room}>{t("rooms.kind.room")}</option>
            <option value={RoomAliasKind.device}>{t("rooms.kind.device")}</option>
          </Select>
        </Field>
        <Button type="submit" pending={add.isPending} aria-label={t("rooms.card.addAliasButton", { name: room.name })}>
          {t("rooms.card.add")}
        </Button>
      </form>
    </div>
  );
}

function RoomDialog({
  open,
  room,
  instances,
  onClose,
}: {
  open: boolean;
  room: Room | undefined;
  instances: Instance[];
  onClose: () => void;
}) {
  const { t } = useTranslation("devices");
  const create = useCreateRoom();
  const update = useUpdateRoom();
  const remove = useDeleteRoom();
  const [name, setName] = useState("");
  const [kodiId, setKodiId] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Start from the room's current values each time the dialog opens, while rendering rather than in an effect.
  const [shown, setShown] = useState<{ open: boolean; room: Room | undefined }>({ open: false, room: undefined });
  if (shown.open !== open || shown.room !== room) {
    setShown({ open, room });
    if (open) {
      setName(room?.name ?? "");
      setKodiId(room?.kodi_instance_id ? String(room.kodi_instance_id) : "");
      setConfirmDelete(false);
    }
  }

  const failure = create.error ?? update.error;
  const close = () => {
    create.reset();
    update.reset();
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      title={room ? t("rooms.dialog.editTitle", { name: room.name }) : t("rooms.dialog.addTitle")}
      footer={
        confirmDelete && room ? (
          <>
            <span className="me-auto self-center">{t("rooms.dialog.confirmRemove", { name: room.name })}</span>
            <Button onClick={() => setConfirmDelete(false)}>{t("rooms.dialog.keep")}</Button>
            <Button
              variant="danger"
              pending={remove.isPending}
              onClick={() => remove.mutate(room.id, { onSuccess: close })}
            >
              {t("rooms.dialog.remove")}
            </Button>
          </>
        ) : (
          <>
            {room ? (
              <Button variant="danger" className="me-auto" onClick={() => setConfirmDelete(true)}>
                {t("rooms.dialog.removeEllipsis")}
              </Button>
            ) : null}
            <Button onClick={close}>{t("rooms.dialog.cancel")}</Button>
            <Button type="submit" form="room-form" variant="primary" pending={create.isPending || update.isPending}>
              {room ? t("rooms.dialog.save") : t("rooms.add")}
            </Button>
          </>
        )
      }
    >
      <form
        id="room-form"
        noValidate
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          const kodi = kodiId ? Number(kodiId) : undefined;
          if (room) {
            update.mutate(
              { body: { kodi_instance_id: kodi ?? null, name: name.trim() }, id: room.id },
              { onSuccess: close },
            );
          } else {
            create.mutate({ name: name.trim(), ...(kodi ? { kodi_instance_id: kodi } : {}) }, { onSuccess: close });
          }
        }}
      >
        <Field label={t("rooms.dialog.fields.name.label")} error={failure ? errorMessage(failure) : undefined}>
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t("rooms.dialog.fields.name.placeholder")}
          />
        </Field>
        <Field label={t("rooms.dialog.fields.kodi.label")} hint={t("rooms.dialog.fields.kodi.hint")}>
          <Select value={kodiId} onChange={(event) => setKodiId(event.target.value)}>
            <option value="">{t("rooms.dialog.fields.kodi.none")}</option>
            {instances.map((instance) => (
              <option key={instance.id} value={instance.id}>
                {instance.name}
              </option>
            ))}
          </Select>
        </Field>
      </form>
    </Dialog>
  );
}
