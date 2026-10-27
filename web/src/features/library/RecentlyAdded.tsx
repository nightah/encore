import { useTranslation } from "react-i18next";

import { useRecentTitles } from "@api/kodi";
import { Chip } from "@components/ui/Chip";
import { ErrorState, Skeleton } from "@components/ui/States";
import { kindLabel, recentStatus, titleWithYear } from "@features/library/labels";
import { formatRelative } from "@lib/format";

export function RecentlyAdded({ libraryId }: { libraryId: number }) {
  const { t } = useTranslation("library");
  const { data, error, isPending } = useRecentTitles(libraryId);
  return (
    <section
      aria-labelledby="ra-h"
      className="flex min-w-0 flex-[2_1_340px] flex-col gap-3 rounded-xl border border-line bg-surface px-5 py-4.5"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="ra-h" className="font-display font-semibold text-lg">
          {t("recent.title")}
        </h2>
        <span className="text-[12.5px] text-muted">{t("recent.live")}</span>
      </div>
      <p className="text-[13px] text-muted">{t("recent.body")}</p>
      {isPending ? <Skeleton className="h-32" /> : null}
      {error ? <ErrorState error={error} /> : null}
      {data && data.length === 0 ? <p className="text-muted">{t("recent.nothing")}</p> : null}
      {data?.length ? (
        <ul className="flex flex-col gap-0.5">
          {data.map((item) => {
            const status = recentStatus(item);
            return (
              <li
                key={`${item.title}-${item.added_at}`}
                className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-2.5 gap-y-0.5 border-line-soft border-b py-2.25"
              >
                <span className="truncate font-medium">{titleWithYear(item)}</span>
                <Chip tone={status.tone}>{status.label}</Chip>
                <span className="text-[12.5px] text-muted">
                  {kindLabel(item.kind)} · {item.instance} · {formatRelative(item.added_at)}
                </span>
              </li>
            );
          })}
        </ul>
      ) : null}
    </section>
  );
}
