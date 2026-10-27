import { API_BASE, ApiError, getCsrfToken, problemMessage } from "@api/client";
import i18n from "@i18n/index";

/**
 * Mutator for the orval-generated client (`orval.config.ts`): resolves paths against the configured base path, sends `X-CSRF-Token` on writes, throws `ApiError` for error responses and resolves to the response body (undefined for 202/204 without one).
 *
 * Generated paths start with `/api/v1`; `API_BASE` already ends with it, so it is stripped once.
 */
export async function orvalFetch<T>(url: string, init: RequestInit = {}): Promise<T> {
  const path = url.replace(/^\/api\/v1/, "");
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const method = (init.method ?? "GET").toUpperCase();
  const csrf = getCsrfToken();
  if (method !== "GET" && csrf) headers.set("X-CSRF-Token", csrf);

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, { ...init, credentials: "same-origin", headers });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError(i18n.t("error.noConnection"), 0, cause);
  }

  let body: unknown;
  if (response.status !== 204) {
    const type = response.headers.get("content-type") ?? "";
    try {
      body = type.includes("json") ? await response.json() : await response.text();
    } catch {
      body = undefined;
    }
  }
  if (!response.ok) throw new ApiError(problemMessage(body, response.status), response.status, body);
  return body as T;
}
