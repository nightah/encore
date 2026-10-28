import { useSyncExternalStore } from "react";

/** Setup wizard progress, saved per browser so the wizard can be skipped and resumed (including across the Login with Amazon redirect). */
export interface SetupProgress {
  /** 1 to 6: the step to show next time. */
  step: number;
  /** Steps finished or skipped. */
  done: number[];
  skipped: number[];
  /** The furthest step shown, so going back to an earlier step can jump forward to it again. */
  reached?: number;
  /** When step 6 started waiting for the first request. */
  waitingSince?: string;
}

export const SETUP_STORAGE_KEY = "encore.setup";
const listeners = new Set<() => void>();
let cache: { raw: string | null; value: SetupProgress | null } = { raw: null, value: null };

function read(): SetupProgress | null {
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(SETUP_STORAGE_KEY);
  } catch {
    raw = null;
  }
  if (raw === cache.raw) return cache.value;
  let value: SetupProgress | null = null;
  try {
    const parsed = raw ? (JSON.parse(raw) as SetupProgress) : null;
    value = parsed && typeof parsed.step === "number" ? parsed : null;
  } catch {
    value = null;
  }
  cache = { raw, value };
  return value;
}

function write(value: SetupProgress | null) {
  try {
    if (value) window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify(value));
    else window.localStorage.removeItem(SETUP_STORAGE_KEY);
  } catch {
    // Without storage the wizard still works; it just can't resume.
  }
  for (const listener of listeners) listener();
}

export function getSetupProgress(): SetupProgress | null {
  return read();
}

export function saveSetupProgress(progress: SetupProgress) {
  write(progress);
}

export function clearSetupProgress() {
  write(null);
}

export function useSetupProgress(): SetupProgress | null {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    read,
    () => null,
  );
}
