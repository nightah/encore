import { useState } from "react";

import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { useDevices, useRooms } from "@api/devices";
import type { Device } from "@api/generated/model";
import { useInstances } from "@api/kodi";
import { useInvocationName } from "@api/skill";
import { Page } from "@components/Page";
import { Button } from "@components/ui/Button";
import { Chip } from "@components/ui/Chip";
import { cn } from "@components/ui/cn";
import { ErrorState, LoadingBlock } from "@components/ui/States";
import { Table, Td, Th } from "@components/ui/Table";
import { DeviceDialog } from "@features/devices/DeviceDialog";
import { RoomsSection } from "@features/devices/Rooms";
import { UnlinkedBanner } from "@features/devices/UnlinkedBanner";
import { useIdentifiedEcho } from "@features/devices/useIdentifiedEcho";
import { formatDayOrClock, truncateDeviceId } from "@lib/format";

export const Route = createFileRoute("/_shell/echo-devices")({
  component: EchoDevicesPage,
});

function EchoDevicesPage() {
  const { t } = useTranslation("devices");
  const invocation = useInvocationName();
  const devices = useDevices();
  const rooms = useRooms();
  const instances = useInstances();
  const [editing, setEditing] = useState<Device | undefined>();
  const instanceList = instances.data ?? [];
  const roomList = rooms.data ?? [];
  // Each Echo's Kodi and room come from the other two lists, so the page waits for all three: an Echo isn't "Not linked" just because the Kodi instances haven't loaded yet.
  const deviceList = instances.data && rooms.data ? devices.data : undefined;
  const queries = [devices, instances, rooms];
  const error = queries.find((q) => q.error)?.error;
  const unlinked = deviceList?.filter((d) => d.kodi_instance_id === null) ?? [];
  const identified = useIdentifiedEcho();
  const identifiedName = devices.data?.find((d) => d.id === identified)?.name;

  return (
    <Page title={t("page.title")} description={t("page.description")}>
      {!deviceList && !error ? <LoadingBlock /> : null}
      {error ? (
        <ErrorState
          error={error}
          onRetry={() => {
            for (const q of queries) if (q.error) void q.refetch();
          }}
        />
      ) : null}

      {unlinked.map((device) => (
        <UnlinkedBanner
          key={device.id}
          device={device}
          instances={instanceList}
          identified={device.id === identified}
        />
      ))}

      {deviceList ? (
        <section aria-labelledby="dev-h" className="flex flex-col rounded-xl border border-line bg-surface">
          <h2 id="dev-h" className="px-4.5 pt-4 pb-1.5 font-display font-semibold text-lg">
            {t("table.title")}
          </h2>
          <p className="px-4.5 pb-2 text-[13px] text-muted">{t("table.identifyHint", { invocation })}</p>
          <p aria-live="polite" className="sr-only">
            {identified !== null ? t("table.spokeTo", { name: identifiedName || t("table.anUnnamedEcho") }) : ""}
          </p>
          {deviceList.length === 0 ? (
            <p className="px-4.5 pt-1 pb-5 text-muted">{t("table.empty")}</p>
          ) : (
            <Table aria-labelledby="dev-h">
              <thead>
                <tr>
                  <Th>{t("table.columns.name")}</Th>
                  <Th>{t("table.columns.room")}</Th>
                  <Th>{t("table.columns.controls")}</Th>
                  <Th>{t("table.columns.lastRequest")}</Th>
                  <Th>{t("table.columns.today")}</Th>
                  <Th className="text-right">
                    <span className="sr-only">{t("table.columns.actions")}</span>
                  </Th>
                </tr>
              </thead>
              <tbody>
                {deviceList.map((device) => {
                  const kodi = instanceList.find((i) => i.id === device.kodi_instance_id);
                  const room = roomList.find((r) => r.id === device.room_id);
                  const name = device.name || t("unnamed");
                  return (
                    <tr
                      key={device.id}
                      className={cn(
                        "[&>td]:align-middle",
                        device.id === identified && "bg-info-bg outline-2 outline-info",
                      )}
                    >
                      <Td>
                        <div className="flex flex-col">
                          <span className="flex flex-wrap items-baseline gap-x-2.5">
                            <span className="font-semibold">{name}</span>
                            <span className="font-mono text-muted text-xs">
                              {truncateDeviceId(device.alexa_device_id)}
                            </span>
                          </span>
                          {device.id === identified ? (
                            <span className="text-[12.5px] text-info">{t("table.identified")}</span>
                          ) : null}
                        </div>
                      </Td>
                      <Td>{room?.name ?? "-"}</Td>
                      <Td>
                        <Chip tone={kodi ? "ok" : "warn"}>{kodi?.name ?? t("notLinked")}</Chip>
                      </Td>
                      <Td className="whitespace-nowrap text-muted">{formatDayOrClock(device.last_seen)}</Td>
                      <Td className="font-mono">{device.request_count_today}</Td>
                      <Td className="text-right">
                        <Button aria-label={t("table.editNamed", { name })} onClick={() => setEditing(device)}>
                          {t("table.edit")}
                        </Button>
                      </Td>
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          )}
        </section>
      ) : null}

      {deviceList ? <RoomsSection rooms={roomList} instances={instanceList} /> : null}

      <DeviceDialog device={editing} onClose={() => setEditing(undefined)} rooms={roomList} instances={instanceList} />
    </Page>
  );
}
