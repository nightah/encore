import { useTranslation } from "react-i18next";

import type { RequestSummary } from "@api/generated/model";
import i18n from "@i18n/index";

/** What Alexa heard for a request, quoted; requests without a title (e.g. "what's playing") show their intent instead. */
export function Heard({ request }: { request: Pick<RequestSummary, "heard" | "intent"> }) {
  const { t } = useTranslation("common");
  if (request.heard) return <>{t("heard.quoted", { text: request.heard })}</>;

  return <span className="font-mono text-[13px] text-muted">{request.intent}</span>;
}

/** Heard as plain text, for announcements: ": “the title”", or nothing when no title was heard. */
export function heardSuffix(request: Pick<RequestSummary, "heard">): string {
  return request.heard ? i18n.t("heard.suffix", { text: request.heard }) : "";
}

/** Who sent a request: the Echo's name, or Amazon's simulator during a deploy's smoke test. */
export function requestSource(request: Pick<RequestSummary, "device_name" | "simulated">): string {
  if (request.simulated) return i18n.t("request.simulator");
  return request.device_name || i18n.t("request.unnamedEcho");
}
