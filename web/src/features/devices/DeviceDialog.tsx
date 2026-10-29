import { useState } from "react";

import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import { useDeleteDevice, useUpdateDevice } from "@api/devices";
import type { Device, Instance, Room } from "@api/generated/model";
import { Button } from "@components/ui/Button";
import { Dialog } from "@components/ui/Dialog";
import { Field, Input, Select } from "@components/ui/Field";
import { truncateDeviceId } from "@lib/format";

interface DeviceDialogProps {
  device: Device | undefined;
  onClose: () => void;
  rooms: Room[];
  instances: Instance[];
}

/** Rename an Echo, put it in a room and choose which Kodi instance it controls. */
export function DeviceDialog({ device, onClose, rooms, instances }: DeviceDialogProps) {
  const { t } = useTranslation("devices");
  const update = useUpdateDevice();
  const remove = useDeleteDevice();
  const [name, setName] = useState("");
  const [roomId, setRoomId] = useState("");
  const [kodiId, setKodiId] = useState("");
  const [confirmForget, setConfirmForget] = useState(false);

  // Start from the Echo's current values whenever another one is opened, while rendering rather than in an effect, so the form never shows the previous Echo's values for a frame.
  const [shown, setShown] = useState<Device>();
  if (device && device !== shown) {
    setShown(device);
    setName(device.name);
    setRoomId(device.room_id === null ? "" : String(device.room_id));
    setKodiId(device.kodi_instance_id === null ? "" : String(device.kodi_instance_id));
    setConfirmForget(false);
  }

  // Every way out of the dialog comes through here, so a failed save never shows on the next Echo opened.
  const close = () => {
    update.reset();
    onClose();
  };

  const title = device?.name ? t("dialog.editTitle", { name: device.name }) : t("dialog.editEcho");

  return (
    <Dialog
      open={!!device}
      onClose={close}
      title={title}
      description={device ? <span className="font-mono">{truncateDeviceId(device.alexa_device_id)}</span> : null}
      footer={
        confirmForget && device ? (
          <>
            <span className="me-auto self-center">{t("dialog.confirmForget")}</span>
            <Button onClick={() => setConfirmForget(false)}>{t("dialog.keep")}</Button>
            <Button
              variant="danger"
              pending={remove.isPending}
              onClick={() => remove.mutate(device.id, { onSuccess: close })}
            >
              {t("dialog.forget")}
            </Button>
          </>
        ) : (
          <>
            <Button variant="danger" className="me-auto" onClick={() => setConfirmForget(true)}>
              {t("dialog.forgetEllipsis")}
            </Button>
            <Button onClick={close}>{t("dialog.cancel")}</Button>
            <Button type="submit" form="device-form" variant="primary" pending={update.isPending}>
              {t("dialog.save")}
            </Button>
          </>
        )
      }
    >
      <form
        id="device-form"
        noValidate
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (!device) return;
          update.mutate(
            {
              body: {
                ...(name.trim() ? { name: name.trim() } : {}),
                kodi_instance_id: kodiId ? Number(kodiId) : null,
                room_id: roomId ? Number(roomId) : null,
              },
              id: device.id,
            },
            { onSuccess: close },
          );
        }}
      >
        <Field label={t("dialog.fields.name.label")} hint={t("dialog.fields.name.hint")}>
          <Input value={name} onChange={(event) => setName(event.target.value)} />
        </Field>
        <Field label={t("dialog.fields.room.label")}>
          <Select value={roomId} onChange={(event) => setRoomId(event.target.value)}>
            <option value="">{t("dialog.fields.room.none")}</option>
            {rooms.map((room) => (
              <option key={room.id} value={room.id}>
                {room.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("controlsKodi")} hint={t("dialog.fields.kodi.hint")}>
          <Select value={kodiId} onChange={(event) => setKodiId(event.target.value)}>
            <option value="">{t("notLinked")}</option>
            {instances.map((instance) => (
              <option key={instance.id} value={instance.id}>
                {instance.name}
              </option>
            ))}
          </Select>
        </Field>
        {update.error ? (
          <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
            {errorMessage(update.error)}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}
