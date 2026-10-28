import { useEffect, useState } from "react";

import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { useDevices } from "@api/devices";
import { useEventStream, useServerEvent } from "@api/events";
import { RequestSummaryOutcome } from "@api/generated/model";
import { useRequests } from "@api/requests";
import { useInvocationName } from "@api/skill";
import { heardSuffix } from "@components/Heard";
import { Page } from "@components/Page";
import { OUTCOME_OPTIONS } from "@components/status";
import { Button } from "@components/ui/Button";
import { cn } from "@components/ui/cn";
import { Input, Select } from "@components/ui/Field";
import { EmptyState, ErrorState, LoadingBlock } from "@components/ui/States";
import { RequestDetailPane } from "@features/requests/RequestDetail";
import { RequestRow } from "@features/requests/RequestList";

const outcomeSchema = z.enum(RequestSummaryOutcome);

const searchSchema = z.object({
  device: z.number().int().optional().catch(undefined),
  id: z.number().int().optional().catch(undefined),
  outcome: outcomeSchema.optional().catch(undefined),
  q: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_shell/request-log")({
  component: RequestLogPage,
  validateSearch: searchSchema,
});

function LiveIndicator() {
  const { t } = useTranslation("requests");
  const status = useEventStream();
  const live = status === "open";
  return (
    <span className="flex items-center gap-2">
      <span
        aria-hidden="true"
        className={cn("size-2 rounded-full", live ? "bg-dot-ok shadow-[0_0_0_4px_rgb(22_163_74/0.18)]" : "bg-dot-warn")}
      />
      {live ? t("live.on") : t("live.off")} · {t("live.retention")}
    </span>
  );
}

function RequestLogPage() {
  const { t } = useTranslation("requests");
  const { t: tc } = useTranslation("common");
  const invocation = useInvocationName();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const devices = useDevices();
  const [query, setQuery] = useState(search.q ?? "");
  const [announcement, setAnnouncement] = useState("");

  const filters = { device: search.device, outcome: search.outcome, q: search.q };
  const { data, error, refetch, isPending, fetchNextPage, hasNextPage, isFetchingNextPage } = useRequests(filters);

  // Debounce the search box into the URL.
  useEffect(() => {
    const next = query.trim() || undefined;
    if (next === search.q) return;
    const timer = setTimeout(() => {
      void navigate({ replace: true, search: (prev) => ({ ...prev, id: undefined, q: next }) });
    }, 300);
    return () => clearTimeout(timer);
  }, [query, search.q, navigate]);

  useServerEvent("request", (request) => {
    setAnnouncement(
      t("newRequest", { device: request.device_name || t("unnamedDevice"), heard: heardSuffix(request) }),
    );
  });

  const items = data?.pages.flatMap((page) => page.items) ?? [];
  const selectedId = search.id ?? items[0]?.id;
  // A filtered Echo that isn't in the device list (still loading, failed to load, or since forgotten) still gets an option, so the filter never reads "All Echos" while the log is narrowed to one.
  const missingDevice =
    search.device !== undefined && !devices.data?.some((device) => device.id === search.device)
      ? search.device
      : undefined;

  return (
    <Page title={t("title")} description={<LiveIndicator />}>
      <search aria-label={t("filters.label")}>
        <form onSubmit={(event) => event.preventDefault()} className="flex flex-wrap gap-2.5">
          <label htmlFor="rq-q" className="sr-only">
            {t("filters.search")}
          </label>
          <Input
            id="rq-q"
            type="search"
            placeholder={t("filters.search")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            className="flex-[1_1_240px] text-[15px]"
          />
          <label htmlFor="rq-device" className="sr-only">
            {t("filters.device")}
          </label>
          <Select
            id="rq-device"
            value={search.device ?? ""}
            onChange={(event) =>
              navigate({
                replace: true,
                search: (prev) => ({
                  ...prev,
                  device: event.target.value ? Number(event.target.value) : undefined,
                  id: undefined,
                }),
              })
            }
            className="w-auto text-[15px]"
          >
            <option value="">{t("filters.allDevices")}</option>
            {missingDevice !== undefined ? (
              <option value={missingDevice}>
                {devices.isPending ? tc("loading") : t("filters.deviceById", { id: missingDevice })}
              </option>
            ) : null}
            {devices.data?.map((device) => (
              <option key={device.id} value={device.id}>
                {device.name || t("filters.unnamedDevice")}
              </option>
            ))}
          </Select>
          <label htmlFor="rq-outcome" className="sr-only">
            {t("filters.outcome")}
          </label>
          <Select
            id="rq-outcome"
            value={search.outcome ?? ""}
            onChange={(event) => {
              const parsed = outcomeSchema.safeParse(event.target.value);
              void navigate({
                replace: true,
                search: (prev) => ({
                  ...prev,
                  id: undefined,
                  outcome: parsed.success ? parsed.data : undefined,
                }),
              });
            }}
            className="w-auto text-[15px]"
          >
            <option value="">{t("filters.allOutcomes")}</option>
            {OUTCOME_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        </form>
      </search>

      <p aria-live="polite" className="sr-only">
        {announcement}
      </p>

      {devices.error ? <ErrorState error={devices.error} onRetry={() => devices.refetch()} /> : null}
      {error ? <ErrorState error={error} onRetry={() => refetch()} /> : null}
      {isPending ? <LoadingBlock label={t("list.loading")} /> : null}

      {data && items.length === 0 ? (
        <EmptyState
          title={filters.q || filters.device || filters.outcome ? t("empty.filteredTitle") : t("empty.noneTitle")}
        >
          {filters.q || filters.device || filters.outcome ? t("empty.filtered") : t("empty.none", { invocation })}
        </EmptyState>
      ) : null}

      {items.length ? (
        <div className="flex flex-wrap items-start gap-4.5">
          <section
            aria-label={t("list.label")}
            className="min-w-0 flex-[1_1_420px] overflow-hidden rounded-xl border border-line bg-surface"
          >
            <ul>
              {items.map((request) => (
                <li key={request.id}>
                  <RequestRow
                    request={request}
                    selected={request.id === selectedId}
                    onSelect={(id) => navigate({ replace: true, search: (prev) => ({ ...prev, id }) })}
                  />
                </li>
              ))}
            </ul>
            {hasNextPage ? (
              <div className="flex justify-center p-3">
                <Button onClick={() => fetchNextPage()} pending={isFetchingNextPage}>
                  {t("list.loadOlder")}
                </Button>
              </div>
            ) : null}
          </section>
          <section
            aria-labelledby="det-h"
            className="min-w-0 flex-[1.3_1_460px] rounded-xl border border-line bg-surface p-5 min-[1000px]:sticky min-[1000px]:top-6"
          >
            {selectedId !== undefined ? <RequestDetailPane key={selectedId} id={selectedId} /> : null}
          </section>
        </div>
      ) : null}
    </Page>
  );
}
