import { LoaderCircle } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import type { DiscoveredInstance as Discovered } from "@api/generated/model";
import { useDiscovered } from "@api/kodi";
import { Button } from "@components/ui/Button";
import { Searching } from "@features/kodi/Searching";
import { formatRelative } from "@lib/format";

/** Kodi instances found with mDNS that Encore doesn't control yet. */
export function DiscoveredList({ onAdd }: { onAdd: (found: Discovered) => void }) {
  const { data, dataUpdatedAt, isPending, isFetching, isError } = useDiscovered();
  const { t } = useTranslation("kodi");

  return (
    <aside
      aria-labelledby="disc-h"
      className="flex min-w-0 flex-[1_1_280px] flex-col gap-3 rounded-xl border border-line bg-surface p-4.5"
    >
      <h2 id="disc-h" className="font-display font-semibold text-lg">
        {t("discovered.title")}
      </h2>
      <p className="text-[13px] text-muted">
        <Trans
          t={t}
          i18nKey="discovered.intro"
          values={{ service: "_xbmc-jsonrpc._tcp" }}
          components={{ mono: <span className="font-mono" /> }}
        />
      </p>
      {isPending ? <Searching /> : null}
      {isError ? <p className="text-bad">{t("discovered.unavailable")}</p> : null}
      {data?.length === 0 ? <p className="text-muted">{t("discovered.none")}</p> : null}
      {data?.map((found) => (
        <div
          key={`${found.host}:${found.http_port}`}
          className="flex items-center justify-between gap-2.5 rounded-[10px] border border-line p-3"
        >
          <div className="flex min-w-0 flex-col">
            <span className="font-semibold">{found.name}</span>
            <span className="font-mono text-[13px] text-muted">
              {found.host}
              {found.version ? ` · ${t("discovered.version", { version: found.version })}` : ""}
            </span>
          </div>
          <Button aria-label={t("discovered.addNamed", { name: found.name })} onClick={() => onAdd(found)}>
            {t("discovered.add")}
          </Button>
        </div>
      ))}
      {data ? (
        <span className="flex items-center gap-1.5 text-[12.5px] text-muted">
          {isFetching ? (
            <>
              <LoaderCircle aria-hidden className="size-3.5 shrink-0 animate-spin" />
              {t("discovered.scanning")}
            </>
          ) : (
            t("discovered.lastScan", { when: formatRelative(new Date(dataUpdatedAt).toISOString()) })
          )}
        </span>
      ) : null}
    </aside>
  );
}
