import { useState } from "react";

import { basePath } from "@src/basePath";

/** Artwork for what's playing: Kodi's poster or thumbnail fetched through Encore, or the title's initials when there's none or it fails to load. */
export function Cover({ artUrl, title }: { artUrl: string; title: string }) {
  const [failedUrl, setFailedUrl] = useState<string>();
  const box = "h-22.5 w-16 flex-none rounded-md";

  if (artUrl && failedUrl !== artUrl) {
    return (
      <img
        src={basePath() + artUrl.replace(/^\//, "")}
        alt=""
        aria-hidden="true"
        loading="lazy"
        onError={() => setFailedUrl(artUrl)}
        className={`${box} bg-[#1e3a4c] object-cover`}
      />
    );
  }

  return (
    <div
      aria-hidden="true"
      className={`${box} flex items-end bg-[#1e3a4c] p-1.5 font-display font-semibold text-[#b8d6e6] text-[11px]`}
    >
      {initials(title)}
    </div>
  );
}

function initials(text: string): string {
  return text
    .replace(/[^A-Za-z]/g, "")
    .slice(0, 3)
    .toUpperCase();
}
