import { useState } from "react";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Library } from "@api/generated/model";
import { keys } from "@api/keys";
import { syncInstance, useInstances, useLibraries } from "@api/kodi";
import { useSettings } from "@api/settings";
import { Page } from "@components/Page";
import { Button, ButtonLink } from "@components/ui/Button";
import { Field, Select } from "@components/ui/Field";
import { EmptyState, ErrorState, LoadingBlock } from "@components/ui/States";
import { MatchTester } from "@features/library/MatchTester";
import { RecentlyAdded } from "@features/library/RecentlyAdded";
import { SummaryTiles } from "@features/library/SummaryTiles";

export const Route = createFileRoute("/_shell/library")({
  component: LibraryPage,
});

/** The shared library first, then the largest. */
function defaultLibrary(libraries: Library[]): Library | undefined {
  return [...libraries].sort(
    (a, b) =>
      b.members.length - a.members.length || b.counts.movies + b.counts.shows - (a.counts.movies + a.counts.shows),
  )[0];
}

function useSyncAll() {
  const { t } = useTranslation("library");
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: t("page.syncStarted") },
    mutationFn: async (libraries: Library[]) => {
      const sources = libraries
        .map((l) => l.source_instance_id || l.members[0]?.instance_id)
        .filter((id): id is number => !!id);
      await Promise.all(sources.map((id) => syncInstance(id)));
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.libraries }),
  });
}

function LibraryPage() {
  const { t } = useTranslation("library");
  const { data: libraries, error, isPending, refetch } = useLibraries();
  const { data: instances } = useInstances();
  const { data: settings, isPending: settingsPending } = useSettings();
  const syncAll = useSyncAll();
  const [selectedId, setSelectedId] = useState<number | undefined>();

  const library = libraries?.find((l) => l.id === selectedId) ?? (libraries ? defaultLibrary(libraries) : undefined);

  return (
    <Page
      title={t("page.title")}
      description={t("page.description")}
      actions={
        <Button
          variant="primary"
          disabled={!libraries?.length}
          pending={syncAll.isPending}
          onClick={() => libraries && syncAll.mutate(libraries)}
        >
          {t("page.syncAll")}
        </Button>
      }
    >
      {isPending ? <LoadingBlock /> : null}
      {error ? <ErrorState error={error} onRetry={() => refetch()} /> : null}
      {libraries && !library ? (
        <EmptyState title={t("empty.title")} action={<ButtonLink to="/kodi">{t("empty.action")}</ButtonLink>}>
          {t("empty.body")}
        </EmptyState>
      ) : null}
      {libraries && library ? (
        <>
          {libraries.length > 1 ? (
            <Field label={t("page.library")} className="max-w-xs">
              <Select value={library.id} onChange={(event) => setSelectedId(Number(event.target.value))}>
                {libraries.map((l) => (
                  <option key={l.id} value={l.id}>
                    {l.name}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          <SummaryTiles
            library={library}
            libraries={libraries}
            instances={instances}
            settings={settings}
            settingsPending={settingsPending}
          />
          <div className="flex flex-wrap items-start gap-4.5">
            <MatchTester key={library.id} libraryId={library.id} />
            <RecentlyAdded libraryId={library.id} />
          </div>
        </>
      ) : null}
    </Page>
  );
}
