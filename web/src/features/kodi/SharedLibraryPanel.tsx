import { useId, useState } from "react";

import type { TFunction } from "i18next";
import { Database } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { Library } from "@api/generated/model";
import { useSetLibrarySource, useSplitLibrary } from "@api/kodi";
import { Button } from "@components/ui/Button";
import { Chip } from "@components/ui/Chip";
import { Dialog } from "@components/ui/Dialog";
import { RadioCards, Select } from "@components/ui/Field";

function explanation(library: Library, t: TFunction<"kodi">): string {
  // Two members read "both"; more read "all of them".
  const n = library.members.length === 2 ? "two" : "many";
  const common = t(`library.explanation.common.${n}`);
  switch (library.state) {
    case "likely_shared":
      return [
        library.evidence
          ? t("library.explanation.likely.evidence", { evidence: library.evidence })
          : t("library.explanation.likely.plain"),
        t(`library.explanation.likely.check.${n}`),
        common,
      ].join(" ");
    case "confirmed_shared":
      return `${t(`library.explanation.confirmed.${n}`)} ${common}`;
    default:
      return `${t("library.explanation.manual")} ${common}`;
  }
}

/** Instances on one Kodi database (e.g. MySQL) share a library: pick the source, or split the group. */
export function SharedLibraryPanel({
  library,
  onAddInstance,
}: {
  library: Library;
  onAddInstance?: (() => void) | undefined;
}) {
  const { t } = useTranslation("kodi");
  const setSource = useSetLibrarySource();
  const split = useSplitLibrary();
  const [splitOpen, setSplitOpen] = useState(false);
  const defaultSplit =
    library.members.find((m) => m.instance_id !== library.source_instance_id)?.instance_id ??
    library.members[0]?.instance_id;
  const [splitId, setSplitId] = useState(String(defaultSplit ?? ""));
  const selectId = useId();
  const headingId = `library-${library.id}-h`;
  // Encore reads from another member while the source is unreachable.
  const fallback =
    library.syncing_instance_id !== library.source_instance_id
      ? library.members.find((m) => m.instance_id === library.syncing_instance_id)
      : undefined;

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 rounded-xl border border-line bg-surface px-5 py-4.5"
    >
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <Database aria-hidden="true" className="size-4.5" />
          <h2 id={headingId} className="font-sans font-semibold text-base">
            {t("library.title", { name: library.name })}
          </h2>
          <Chip tone={library.state === "confirmed_shared" ? "ok" : "outline"}>
            {t(`library.state.${library.state}`)}
          </Chip>
        </div>
        <div className="flex flex-wrap gap-2">
          {onAddInstance ? <Button onClick={onAddInstance}>{t("library.share.add")}</Button> : null}
          <Button
            onClick={() => {
              setSplitId(String(defaultSplit ?? ""));
              setSplitOpen(true);
            }}
          >
            {t("library.split.open")}
          </Button>
        </div>
      </div>
      <p className="text-muted">{explanation(library, t)}</p>
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1.5">
          <label htmlFor={selectId} className="font-medium">
            {t("library.source")}
          </label>
          <Select
            id={selectId}
            className="w-auto min-w-40"
            value={String(library.source_instance_id)}
            disabled={setSource.isPending}
            onChange={(event) => setSource.mutate({ id: library.id, sourceInstanceId: Number(event.target.value) })}
          >
            {library.members.map((member) => (
              <option key={member.instance_id} value={member.instance_id}>
                {member.name}
              </option>
            ))}
          </Select>
        </div>
        <span className="max-w-[52ch] text-[13px] text-muted" aria-live="polite">
          {fallback ? t("library.fallback", { name: fallback.name }) : t("library.sourceHint")}
        </span>
      </div>

      <Dialog
        open={splitOpen}
        onClose={() => setSplitOpen(false)}
        title={t("library.split.title")}
        description={t("library.split.description")}
        footer={
          <>
            <Button onClick={() => setSplitOpen(false)}>{t("library.split.cancel")}</Button>
            <Button
              variant="primary"
              pending={split.isPending}
              onClick={() =>
                split.mutate({ id: library.id, instanceId: Number(splitId) }, { onSuccess: () => setSplitOpen(false) })
              }
            >
              {t("library.split.confirm")}
            </Button>
          </>
        }
      >
        <RadioCards
          legend={t("library.split.legend")}
          value={splitId}
          onValueChange={setSplitId}
          options={library.members.map((member) => ({
            hint: member.instance_id === library.source_instance_id ? t("library.split.currentSource") : undefined,
            label: member.name,
            value: String(member.instance_id),
          }))}
        />
      </Dialog>
    </section>
  );
}
