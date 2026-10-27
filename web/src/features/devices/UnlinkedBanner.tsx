import { useState } from "react";

import { useQuery } from "@tanstack/react-query";
import { Trans, useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import { useUpdateDevice } from "@api/devices";
import type { Device, Instance } from "@api/generated/model";
import { listRequests } from "@api/requests";
import { heardSuffix } from "@components/Heard";
import { Button } from "@components/ui/Button";
import { cn } from "@components/ui/cn";
import { Field, Input, Select } from "@components/ui/Field";
import { formatDayOrClock, truncateDeviceId } from "@lib/format";

function isToday(iso: string): boolean {
  return new Date(iso).toDateString() === new Date().toDateString();
}

/** Main call to action: a new Echo has spoken but doesn't control a Kodi instance yet. */
export function UnlinkedBanner({
  device,
  instances,
  identified = false,
}: {
  device: Device;
  instances: Instance[];
  /** Someone just asked this Echo "which Echo is this?". */
  identified?: boolean;
}) {
  const { t } = useTranslation("devices");
  const [name, setName] = useState(device.name);
  const [kodiId, setKodiId] = useState(String(instances[0]?.id ?? ""));
  const update = useUpdateDevice(t("unlinked.linked"));
  const firstRequest = useQuery({
    queryFn: ({ signal }) => listRequests({ device: device.id, limit: 50 }, { signal }),
    queryKey: ["requests", "device-first", device.id],
    select: (page) => page.items.at(-1),
  });
  const headingId = `unlinked-${device.id}-h`;

  return (
    <section
      aria-labelledby={headingId}
      className={cn(
        "flex flex-wrap items-center justify-between gap-4 rounded-xl bg-warn-soft px-4.5 py-4",
        identified && "outline-2 outline-info",
      )}
    >
      <div className="flex min-w-0 flex-col gap-0.5">
        <h2 id={headingId} className="font-sans font-semibold text-base">
          {identified ? t("unlinked.titleIdentified") : t("unlinked.title")}
        </h2>
        <span className="text-muted">
          <Trans
            t={t}
            i18nKey={isToday(device.first_seen) ? "unlinked.firstRequest.today" : "unlinked.firstRequest.other"}
            values={{
              heard: firstRequest.data ? heardSuffix(firstRequest.data) : "",
              id: truncateDeviceId(device.alexa_device_id),
              when: formatDayOrClock(device.first_seen),
            }}
            components={{ mono: <span className="font-mono text-[13px]" /> }}
            // What the Echo heard is user data: escape it before Trans parses its tags, then unescape the text.
            shouldUnescape
            tOptions={{ interpolation: { escapeValue: true } }}
          />
        </span>
        {update.error ? (
          <span role="alert" className="text-bad text-sm">
            {errorMessage(update.error)}
          </span>
        ) : null}
      </div>
      <form
        className="flex flex-wrap items-center gap-2.5"
        onSubmit={(event) => {
          event.preventDefault();
          update.mutate({
            body: {
              ...(name.trim() ? { name: name.trim() } : {}),
              kodi_instance_id: Number(kodiId),
            },
            id: device.id,
          });
        }}
      >
        <Field label={t("unlinked.deviceName")} hideLabel>
          <Input
            className="w-44"
            placeholder={t("unlinked.placeholder")}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>
        <Field label={t("controlsKodi")} hideLabel>
          <Select className="w-auto" value={kodiId} onChange={(event) => setKodiId(event.target.value)}>
            {instances.map((instance) => (
              <option key={instance.id} value={instance.id}>
                {instance.name}
              </option>
            ))}
          </Select>
        </Field>
        <Button type="submit" variant="primary" pending={update.isPending} disabled={!kodiId}>
          {t("unlinked.link")}
        </Button>
      </form>
    </section>
  );
}
