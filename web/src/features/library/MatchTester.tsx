import { type FormEvent, useState } from "react";

import { X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { MatchResult } from "@api/generated/model";
import { useAliases, useCreateAlias, useDeleteAlias, useMatch } from "@api/library";
import { Button } from "@components/ui/Button";
import { Label } from "@components/ui/Card";
import { Chip, ToggleChip } from "@components/ui/Chip";
import { cn, focusRing } from "@components/ui/cn";
import { Field, Input, Select } from "@components/ui/Field";
import { Table, Td, Th } from "@components/ui/Table";
import { EXAMPLES, MEDIA_TYPES, aliasKind, boostText, explain, titleWithYear, verdict } from "@features/library/labels";
import { formatScore } from "@lib/format";

/** Type what Alexa heard and see how Encore ranks it (POST /match). */
export function MatchTester({ libraryId }: { libraryId: number }) {
  const { t } = useTranslation("library");
  const [query, setQuery] = useState("");
  const [type, setType] = useState("any");
  const [tested, setTested] = useState<{ query: string; result: MatchResult } | null>(null);
  const match = useMatch();
  const createAlias = useCreateAlias();

  async function run(nextQuery: string, nextType: string) {
    if (!nextQuery.trim()) return;
    const kinds = MEDIA_TYPES.find((m) => m.value === nextType)?.kinds;
    try {
      const result = await match.mutateAsync({
        library_id: libraryId,
        query: nextQuery.trim(),
        ...(kinds ? { kinds } : {}),
      });
      setTested({ query: nextQuery.trim(), result });
    } catch {
      setTested(null);
    }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    void run(query, type);
  }

  const top = tested?.result.candidates[0];
  const topKind = top ? aliasKind(top.kind) : undefined;
  // What an alias for the top result would point at, if the server can name it.
  const aliasTarget = top && top.media_id > 0 && topKind ? { kind: topKind, media_id: top.media_id } : undefined;

  return (
    <section
      aria-labelledby="mt-h"
      className="flex min-w-0 flex-[3_1_540px] flex-col gap-4 rounded-xl border border-line bg-surface px-5 py-4.5"
    >
      <h2 id="mt-h" className="font-display font-semibold text-lg">
        {t("matchTester.title")}
      </h2>
      <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2.5">
        <Field label={t("matchTester.heard")} hideLabel className="flex-[1_1_260px]">
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("matchTester.heard")}
          />
        </Field>
        <Field label={t("matchTester.mediaType")} hideLabel>
          <Select value={type} onChange={(event) => setType(event.target.value)}>
            {MEDIA_TYPES.map((m) => (
              <option key={m.value} value={m.value}>
                {t(`matchTester.types.${m.value}`)}
              </option>
            ))}
          </Select>
        </Field>
        <Button type="submit" variant="primary" pending={match.isPending}>
          {t("matchTester.test")}
        </Button>
      </form>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[13px] text-muted">{t("matchTester.try")}</span>
        {EXAMPLES.map((example) => (
          <ToggleChip
            key={example.query}
            pressed={tested?.query === example.query}
            onClick={() => {
              setQuery(example.query);
              setType(example.type);
              void run(example.query, example.type);
            }}
          >
            {example.query}
          </ToggleChip>
        ))}
      </div>

      <div aria-live="polite" className="flex flex-col gap-4">
        {match.error ? (
          <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
            {errorMessage(match.error)}
          </p>
        ) : null}
        {tested ? (
          <>
            <div className="flex flex-wrap items-center gap-2.5 rounded-[10px] bg-soft px-3.5 py-3">
              <Chip tone={verdict(tested.result.decision).tone}>{verdict(tested.result.decision).label}</Chip>
              <span>{explain(tested.result)}</span>
            </div>
            <div className="flex flex-col gap-1">
              <Label>{t("matchTester.normalized")}</Label>
              <span className="font-mono text-[13px]">{tested.result.normalized}</span>
            </div>
            {tested.result.candidates.length ? (
              <Table aria-label={t("matchTester.candidates")}>
                <thead>
                  <tr>
                    <Th>{t("matchTester.columns.candidate")}</Th>
                    <Th>{t("matchTester.columns.alias")}</Th>
                    <Th>{t("matchTester.columns.tokenSet")}</Th>
                    <Th>Jaro-W</Th>
                    <Th>{t("matchTester.columns.phonetic")}</Th>
                    <Th>{t("matchTester.columns.boost")}</Th>
                    <Th>{t("matchTester.columns.score")}</Th>
                  </tr>
                </thead>
                <tbody>
                  {tested.result.candidates.map((candidate) => (
                    <tr key={`${candidate.title}-${candidate.year}-${candidate.media_id}`}>
                      <Td className="font-medium">{titleWithYear(candidate)}</Td>
                      <Td className="font-mono text-muted">{candidate.breakdown.alias || "-"}</Td>
                      <Td className="font-mono">{formatScore(candidate.breakdown.token_set)}</Td>
                      <Td className="font-mono">{formatScore(candidate.breakdown.jaro_winkler)}</Td>
                      <Td className="font-mono">{formatScore(candidate.breakdown.phonetic)}</Td>
                      <Td className="whitespace-nowrap font-mono">{boostText(candidate)}</Td>
                      <Td className="font-mono font-semibold">{formatScore(candidate.score)}</Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : null}
            {top ? (
              <div className="flex flex-wrap items-center gap-2.5">
                <Button
                  disabled={!aliasTarget}
                  pending={createAlias.isPending}
                  onClick={
                    aliasTarget
                      ? () =>
                          createAlias.mutate({
                            ...aliasTarget,
                            alias: tested.query.toLowerCase(),
                            library_id: libraryId,
                          })
                      : undefined
                  }
                >
                  {t("matchTester.addAlias", { query: tested.query })}
                </Button>
                <span className="text-[13px] text-muted">
                  {aliasTarget ? t("matchTester.aliasHint") : t("matchTester.noAlias")}
                </span>
              </div>
            ) : null}
          </>
        ) : null}
      </div>

      <AliasList libraryId={libraryId} />
    </section>
  );
}

function AliasList({ libraryId }: { libraryId: number }) {
  const { t } = useTranslation("library");
  const { data: aliases } = useAliases(libraryId);
  const remove = useDeleteAlias();
  if (!aliases?.length) return null;
  return (
    <div className="flex flex-col gap-2 border-line-soft border-t pt-4">
      <h3 className="font-sans font-semibold">{t("matchTester.aliases")}</h3>
      <ul className="flex flex-wrap gap-2">
        {aliases.map((alias) => (
          <li
            key={alias.id}
            className="inline-flex items-center gap-1 rounded-full bg-neutral-bg py-0.5 pr-0.5 pl-3 text-[13px] text-neutral"
          >
            <span>{t("matchTester.aliasEntry", { alias: alias.alias, title: alias.title })}</span>
            <button
              type="button"
              aria-label={t("matchTester.removeAlias", { alias: alias.alias })}
              onClick={() => remove.mutate(alias.id)}
              className={cn(
                "relative inline-flex size-7 items-center justify-center rounded-full hover:bg-surface after:absolute after:-inset-2 after:content-['']",
                focusRing,
              )}
            >
              <X aria-hidden="true" className="size-3.5" />
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
