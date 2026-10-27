import { useEffect, useState } from "react";

import { useServerEvent } from "@api/events";

/** How long an Echo stays highlighted after "Alexa, ask encore which device this is". */
export const IDENTIFY_HIGHLIGHT_MS = 20_000;

/** The ID (in /devices) of the Echo someone just asked "which Echo is this?", for a short while; null otherwise. */
export function useIdentifiedEcho(): number | null {
  const [identified, setIdentified] = useState<number | null>(null);

  useServerEvent("request", (request) => {
    if (request.intent === "IdentifyEcho" && request.device_id !== null) setIdentified(request.device_id);
  });

  useEffect(() => {
    if (identified === null) return;
    const timer = setTimeout(() => setIdentified(null), IDENTIFY_HIGHLIGHT_MS);
    return () => clearTimeout(timer);
  }, [identified]);

  return identified;
}
