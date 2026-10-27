import { LoaderCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Callout } from "@components/ui/States";

/** Shown while mDNS looks for Kodi, which takes a few seconds. */
export function Searching() {
  const { t } = useTranslation("kodi");
  return (
    <Callout tone="soft">
      <span role="status" className="flex items-center gap-2">
        <LoaderCircle aria-hidden className="size-4 shrink-0 animate-spin" />
        {t("discovered.searching")}
      </span>
    </Callout>
  );
}
