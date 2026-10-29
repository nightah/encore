import { useState } from "react";

import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { DiscoveredInstance, Instance } from "@api/generated/model";
import { useInstances, useLibraries } from "@api/kodi";
import { Page } from "@components/Page";
import { Button } from "@components/ui/Button";
import { EmptyState, ErrorState, LoadingBlock } from "@components/ui/States";
import { DiscoveredList } from "@features/kodi/DiscoveredList";
import { InstanceCard } from "@features/kodi/InstanceCard";
import { InstanceDialog, type InstanceForm } from "@features/kodi/InstanceDialog";
import { SharedLibraryPanel } from "@features/kodi/SharedLibraryPanel";
import { ShareLibraryDialog } from "@features/kodi/ShareLibraryDialog";

export const Route = createFileRoute("/_shell/kodi")({
  component: KodiPage,
});

type DialogState =
  | { mode: "closed" }
  | { mode: "add"; prefill?: Partial<InstanceForm> }
  | { mode: "edit"; instance: Instance };

function prefillFrom(found: DiscoveredInstance): Partial<InstanceForm> {
  return {
    name: found.name,
    url: `http://${found.host}:${found.http_port}/jsonrpc`,
    websocket_url: found.ws_port ? `ws://${found.host}:${found.ws_port}/jsonrpc` : "",
  };
}

function KodiPage() {
  const { t } = useTranslation("kodi");
  const instances = useInstances();
  const libraries = useLibraries();
  const [dialog, setDialog] = useState<DialogState>({ mode: "closed" });
  const [share, setShare] = useState<{ open: boolean; initial?: number[] }>({ open: false });
  // A card names the instances it shares a library with, so the cards wait for the libraries too rather than showing "Shared with" no one.
  const list = libraries.data ? instances.data : undefined;
  const error = instances.error ?? libraries.error;
  const shared = (list && libraries.data?.filter((l) => l.members.length > 1)) ?? [];
  const libraryOf = (instance: Instance) =>
    libraries.data?.find((l) => l.members.some((m) => m.instance_id === instance.id));

  return (
    <Page
      title={t("page.title")}
      description={t("page.description")}
      actions={
        <>
          {(instances.data?.length ?? 0) > 1 ? (
            <Button onClick={() => setShare({ open: true })}>{t("library.share.open")}</Button>
          ) : null}
          <Button variant="primary" onClick={() => setDialog({ mode: "add" })}>
            {t("page.addManually")}
          </Button>
        </>
      }
    >
      <div className="flex flex-wrap items-start gap-4.5">
        <div className="flex min-w-0 flex-[3_1_560px] flex-col gap-4">
          {!list && !error ? <LoadingBlock /> : null}
          {error ? (
            <ErrorState
              error={error}
              onRetry={() => {
                for (const q of [instances, libraries]) if (q.error) void q.refetch();
              }}
            />
          ) : null}
          {shared.map((library) => (
            <SharedLibraryPanel
              key={library.id}
              library={library}
              onAddInstance={
                (list?.length ?? 0) > library.members.length
                  ? () => setShare({ initial: library.members.map((m) => m.instance_id), open: true })
                  : undefined
              }
            />
          ))}
          {list?.length ? (
            <div className="grid items-start gap-4 min-[1800px]:grid-cols-2 min-[2800px]:grid-cols-3">
              {list.map((instance) => (
                <InstanceCard
                  key={instance.id}
                  instance={instance}
                  library={libraryOf(instance)}
                  onEdit={() => setDialog({ instance, mode: "edit" })}
                />
              ))}
            </div>
          ) : null}
          {list?.length === 0 ? (
            <EmptyState
              title={t("page.empty.title")}
              action={
                <Button variant="primary" onClick={() => setDialog({ mode: "add" })}>
                  {t("page.addManually")}
                </Button>
              }
            >
              {t("page.empty.body")}
            </EmptyState>
          ) : null}
        </div>
        <DiscoveredList onAdd={(found) => setDialog({ mode: "add", prefill: prefillFrom(found) })} />
      </div>

      <ShareLibraryDialog
        open={share.open}
        onClose={() => setShare((current) => ({ ...current, open: false }))}
        instances={instances.data ?? []}
        libraries={libraries.data ?? []}
        initial={share.initial}
      />

      <InstanceDialog
        open={dialog.mode !== "closed"}
        onClose={() => setDialog({ mode: "closed" })}
        // A snapshot from when the dialog opened, so live updates don't reset the form mid-edit.
        instance={dialog.mode === "edit" ? dialog.instance : undefined}
        prefill={dialog.mode === "add" ? dialog.prefill : undefined}
      />
    </Page>
  );
}
