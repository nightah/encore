import { useTranslation } from "react-i18next";

import type { Instance, Library, Settings } from "@api/generated/model";
import { StatTile } from "@components/ui/Card";
import i18n from "@i18n/index";
import { formatNumber, formatRelative, hostPort } from "@lib/format";

function snapshot(settings: Settings | undefined): { label: string; where?: string } {
  if (!settings) return { label: "-" };
  const { backend, s3, webdav, local } = settings.storage;
  if (backend === "s3") {
    return { label: i18n.t("library:summary.s3"), where: s3.endpoint ? hostPort(s3.endpoint) : "AWS S3" };
  }
  if (backend === "webdav") return { label: "WebDAV", where: hostPort(webdav.url) };
  return { label: i18n.t("library:summary.localDisk"), where: local.path };
}

export function SummaryTiles({
  library,
  libraries,
  instances,
  settings,
}: {
  library: Library;
  libraries: Library[];
  instances: Instance[] | undefined;
  settings: Settings | undefined;
}) {
  const { t } = useTranslation("library");
  const source = instances?.find((i) => i.id === library.source_instance_id);
  const episodes = source?.library?.episodes;
  const synced = libraries
    .map((l) => l.last_sync_at)
    .filter((t): t is string => !!t)
    .sort()
    .at(-1);
  const lastBackup = settings?.storage.backups.last_backup_at;
  const store = snapshot(settings);
  return (
    <section aria-label={t("summary.label")} className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-3">
      <StatTile label={t("summary.movies")} value={formatNumber(library.counts.movies)} />
      <StatTile
        label={t("summary.shows")}
        value={formatNumber(library.counts.shows)}
        detail={episodes !== undefined ? t("summary.episodes", { count: episodes }) : undefined}
      />
      <StatTile label={t("summary.music")} value={formatNumber(library.counts.songs)} detail={t("summary.songs")} />
      <StatTile
        label={t("summary.libraries")}
        value={formatNumber(libraries.length)}
        detail={synced ? t("summary.synced", { when: formatRelative(synced) }) : t("summary.notSynced")}
      />
      <StatTile
        label={t("summary.backups")}
        value={store.label}
        detail={[
          store.where,
          lastBackup ? t("summary.lastBackup", { when: formatRelative(lastBackup) }) : t("summary.noBackup"),
        ]
          .filter(Boolean)
          .join(" · ")}
      />
    </section>
  );
}
