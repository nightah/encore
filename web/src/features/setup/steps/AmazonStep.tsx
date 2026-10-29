import { type FormEvent, type ReactNode, useState } from "react";

import { ExternalLink } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { ApiError, errorMessage } from "@api/client";
import { getAuthorize, useAuthorize, usePasteAmazon, useSaveAmazonCredentials, useSkill } from "@api/skill";
import { Button } from "@components/ui/Button";
import { Field, Input } from "@components/ui/Field";
import { Callout, CopyButton } from "@components/ui/States";
import { StepFooter, StepHeading, StepPending, type StepProps } from "@features/setup/shared";
import { pageLocation } from "@lib/pageLocation";

const LWA_CONSOLE = "https://developer.amazon.com/loginwithamazon/console/site/lwa/overview.html";
const LWA_TOKENS_COMMAND = "ask util generate-lwa-tokens";

function fallbackReturnUrls(endpoint: string | undefined): string[] {
  try {
    const origin = endpoint ? new URL(endpoint).origin : window.location.origin;
    return [`${origin}/api/v1/oauth/lwa/callback`];
  } catch {
    return [];
  }
}

/** Reads the error Login with Amazon came back with, once: it is removed from the address so a reload doesn't show it again. */
function takeAmazonError(): string | null {
  const url = new URL(window.location.href);
  const error = url.searchParams.get("amazon_error");
  if (url.searchParams.has("amazon_error") || url.searchParams.has("amazon")) {
    url.searchParams.delete("amazon_error");
    url.searchParams.delete("amazon");
    window.history.replaceState(window.history.state, "", url);
  }
  return error;
}

/** The link to the Login with Amazon console; Trans passes the translated link text as children. */
function ConsoleLink({ children }: { children?: ReactNode }) {
  const { t } = useTranslation("setup");
  return (
    <a href={LWA_CONSOLE} target="_blank" rel="noreferrer noopener" className="text-link hover:text-ink">
      {children}
      <ExternalLink aria-hidden="true" className="ms-1 inline size-3.5" />
      <span className="sr-only">{t("amazon.newTab")}</span>
    </a>
  );
}

export function AmazonStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const skillQuery = useSkill();
  const skill = skillQuery.data;
  const authorize = useAuthorize(!!skill && !skill.amazon.connected);
  const saveCredentials = useSaveAmazonCredentials();
  const [clientId, setClientId] = useState<string | null>(null);
  const [clientSecret, setClientSecret] = useState("");
  const [redirecting, setRedirecting] = useState(false);
  // Login with Amazon comes back to /setup, with ?amazon_error= when it failed.
  const [redirectError, setRedirectError] = useState(() => {
    const error = takeAmazonError();
    return error ? t("amazon.redirectError", { error }) : null;
  });

  // Whether Amazon is connected comes from the skill, so the step waits for it rather than asking to connect an account that already is.
  if (!skill) {
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("amazon.heading")}</StepHeading>
        <StepPending error={skillQuery.error} onRetry={() => skillQuery.refetch()} />
        <StepFooter nav={nav} primary={null} />
      </div>
    );
  }

  if (skill.amazon.connected && !skill.amazon.needs_reauthorise) {
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("amazon.heading")}</StepHeading>
        <Callout tone="ok">
          {skill.amazon.vendor_id
            ? t("amazon.connectedVendor", { vendor: skill.amazon.vendor_id })
            : t("amazon.connected")}
        </Callout>
        <StepFooter
          nav={nav}
          primary={
            <Button variant="primary" onClick={nav.done}>
              {t("nav.continue")}
            </Button>
          }
        />
      </div>
    );
  }

  const allReturnUrls = authorize.data?.return_urls ?? fallbackReturnUrls(skill.endpoint.url);
  // Over HTTPS Amazon returns to Encore's callback; without HTTPS it returns to the local address for the paste flow, as Amazon allows plain HTTP only for loopback.
  const https = allReturnUrls.some((url) => url.startsWith("https://"));
  const returnUrls = allReturnUrls.filter((url) => url.startsWith(https ? "https://" : "http://127.0.0.1"));
  const id = clientId ?? skill.amazon.profile_client_id ?? "";
  const fieldErrors = saveCredentials.error instanceof ApiError ? saveCredentials.error.fieldErrors : {};

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setRedirectError(null);
    try {
      // The submit button stays disabled without a secret, so there always is one here.
      await saveCredentials.mutateAsync({ client_id: id.trim(), client_secret: clientSecret });
      setRedirecting(true);
      const { url } = await getAuthorize("setup");
      pageLocation.assign(url);
    } catch (error) {
      setRedirecting(false);
      if (!(error instanceof ApiError && Object.keys(error.fieldErrors).length)) {
        setRedirectError(errorMessage(error));
      }
    }
  };

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4.5">
      <StepHeading intro={t("amazon.intro")}>{t("amazon.heading")}</StepHeading>
      {skill.amazon.needs_reauthorise ? <Callout tone="warnStrong">{t("amazon.reauthorise")}</Callout> : null}
      <ol className="flex list-decimal flex-col gap-2 ps-5">
        <li>
          <Trans t={t} i18nKey="amazon.instructions.open" components={{ link: <ConsoleLink /> }} />
        </li>
        <li>
          {t("amazon.instructions.returnUrls", { count: Math.max(returnUrls.length, 1) })}
          <ul className="mt-1.5 flex flex-col gap-1">
            {returnUrls.map((url) => (
              <li key={url} className="flex items-center gap-1">
                <span className="wrap-anywhere rounded bg-soft px-1.5 py-0.5 font-mono text-[13px]">{url}</span>
                <CopyButton text={url} label={t("amazon.copyReturnUrl")} />
              </li>
            ))}
          </ul>
        </li>
        <li>{t("amazon.instructions.credentials")}</li>
      </ol>
      <Field label={t("amazon.clientId")} error={fieldErrors.client_id}>
        <Input
          mono
          autoComplete="off"
          spellCheck={false}
          placeholder="amzn1.application-oa2-client.…"
          value={id}
          onChange={(e) => setClientId(e.target.value)}
        />
      </Field>
      <Field
        label={t("amazon.clientSecret.label")}
        hint={t("amazon.clientSecret.hint")}
        error={fieldErrors.client_secret}
      >
        <Input
          type="password"
          autoComplete="off"
          value={clientSecret}
          onChange={(e) => setClientSecret(e.target.value)}
        />
      </Field>
      {redirectError ? (
        <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
          {redirectError}
        </p>
      ) : null}

      <PasteFallbacks https={https} />

      <StepFooter
        nav={nav}
        primary={
          <Button
            type="submit"
            variant="primary"
            pending={saveCredentials.isPending || redirecting}
            disabled={!id.trim() || !clientSecret}
          >
            {t("amazon.authorize")}
          </Button>
        }
      />
    </form>
  );
}

/** For setups without an HTTPS callback: paste the redirect URL, or a refresh token from the ASK CLI. Over HTTPS the callback finishes sign-in itself, so only the refresh token applies. */
function PasteFallbacks({ https }: { https: boolean }) {
  const { t } = useTranslation("setup");
  const paste = usePasteAmazon();
  // Over HTTPS only the refresh token applies; `https` can change after the first render, once the skill and its return URLs load.
  const [chosen, setMode] = useState<"redirect_url" | "refresh_token">("redirect_url");
  const mode = https ? "refresh_token" : chosen;
  const [value, setValue] = useState("");
  const error =
    paste.error instanceof ApiError ? (paste.error.fieldErrors[mode] ?? errorMessage(paste.error)) : undefined;
  const connect = () =>
    paste.mutate(mode === "redirect_url" ? { redirect_url: value.trim() } : { refresh_token: value.trim() });

  return (
    <details className="rounded-[10px] border border-line px-4 py-3">
      <summary className="min-h-11 cursor-pointer content-center text-[13px] text-muted">
        <Trans
          t={t}
          i18nKey={https ? "amazon.fallback.summaryHttps" : "amazon.fallback.summaryHttp"}
          values={{ command: LWA_TOKENS_COMMAND }}
          components={{ code: <span className="font-mono" /> }}
        />
      </summary>
      <div className="mt-2 flex flex-col gap-3">
        {https ? null : (
          <div role="radiogroup" aria-label={t("amazon.fallback.label")} className="flex flex-wrap gap-2">
            {(
              [
                ["redirect_url", t("amazon.fallback.redirectUrl.option")],
                ["refresh_token", t("amazon.fallback.refreshToken.option")],
              ] as const
            ).map(([key, label]) => (
              <Button
                key={key}
                role="radio"
                aria-checked={mode === key}
                variant={mode === key ? "primary" : "default"}
                onClick={() => {
                  setMode(key);
                  setValue("");
                  paste.reset();
                }}
              >
                {label}
              </Button>
            ))}
          </div>
        )}
        <Field
          label={
            mode === "redirect_url" ? t("amazon.fallback.redirectUrl.label") : t("amazon.fallback.refreshToken.label")
          }
          hint={
            mode === "redirect_url"
              ? t("amazon.fallback.redirectUrl.hint")
              : t("amazon.fallback.refreshToken.hint", { prefix: "Atzr|" })
          }
          error={error}
        >
          <Input
            mono
            autoComplete="off"
            spellCheck={false}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              // Enter here connects with the pasted value instead of submitting the outer credentials form.
              if (e.key !== "Enter") return;
              e.preventDefault();
              if (value.trim()) connect();
            }}
          />
        </Field>
        <div>
          <Button pending={paste.isPending} disabled={!value.trim()} onClick={connect}>
            {t("amazon.fallback.connect")}
          </Button>
        </div>
      </div>
    </details>
  );
}
