import { useId } from "react";

import { useTranslation } from "react-i18next";

import { CopyButton } from "@components/ui/States";

/** A titled block of configuration to copy into another program; the figure is named by its title. */
export function CodeBlock({ title, code, label }: { title: string; code: string; label: string }) {
  const { t } = useTranslation("common");
  const id = useId();
  return (
    <figure aria-labelledby={id} className="m-0 flex min-w-0 flex-col gap-1.5">
      <figcaption className="flex items-center justify-between gap-2 font-medium text-sm">
        <span id={id}>{title}</span>
        <CopyButton text={code} label={t("actions.copyNamed", { label })} speakText={false} />
      </figcaption>
      <pre className="overflow-x-auto rounded-[10px] bg-[#161d26] px-4 py-3.5 font-mono text-[#c3ccd6] text-[13px] leading-relaxed">
        {code}
      </pre>
    </figure>
  );
}
