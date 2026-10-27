import type { Deploy, DeployEventStep as DeployStep, SkillInfo as Skill } from "@api/generated/model";
import i18n from "@i18n/index";
import { formatClock } from "@lib/format";

export type MarkState = "ok" | "busy" | "fail" | "pending";

export interface TimelineItem {
  title: string;
  detail: string;
  state: MarkState;
}

export const DEPLOY_STEPS: DeployStep[] = [
  "package",
  "vendor",
  "skill",
  "upload",
  "import",
  "build",
  "enable",
  "simulate",
];

/** "amzn1.ask.skill.3f2a…91c0" */
export function shortId(id: string, keep = 4): string {
  const cut = id.lastIndexOf(".") + 1;
  const prefix = id.slice(0, cut);
  const rest = id.slice(cut);
  if (rest.length <= keep * 2 + 1) return id;
  return `${prefix}${rest.slice(0, keep)}…${rest.slice(-keep)}`;
}

function shortVendor(vendor: string): string {
  return vendor.length > 6 ? `${vendor.slice(0, 4)}…${vendor.slice(-2)}` : vendor;
}

function stripScheme(url: string): string {
  return url.replace(/^https?:\/\//, "");
}

function stepDone(deploy: Deploy | undefined, step: DeployStep): boolean {
  return !!deploy?.events.some((e) => e.step === step && (e.state === "succeeded" || e.state === "skipped"));
}

/** The five-step status strip at the top of the Alexa skill page. */
export function buildTimeline(skill: Skill, deploy: Deploy | undefined): TimelineItem[] {
  const running = deploy?.state === "in_progress";
  const failed = deploy?.state === "failed" || (!deploy && skill.last_deploy?.state === "failed");
  const enabled = skill.locales_enabled.join(", ");
  const last = skill.last_deploy;
  const t = i18n.getFixedT(null, "skill", "timeline");

  const amazon: TimelineItem = skill.amazon.connected
    ? skill.amazon.needs_reauthorise
      ? { detail: t("amazon.reauthorise"), state: "fail", title: t("amazon.title") }
      : {
          detail: skill.amazon.vendor_id
            ? t("amazon.vendor", { vendor: shortVendor(skill.amazon.vendor_id) })
            : t("amazon.connected"),
          state: "ok",
          title: t("amazon.title"),
        }
    : { detail: t("amazon.notConnected"), state: "fail", title: t("amazon.title") };

  const created: TimelineItem = skill.skill_id
    ? { detail: shortId(skill.skill_id), state: "ok", title: t("created.title") }
    : {
        detail: running ? t("created.creating") : t("created.firstDeploy"),
        state: running ? "busy" : "pending",
        title: t("created.title"),
      };

  const endpoint: TimelineItem = {
    detail: stripScheme(skill.endpoint.url),
    state: "ok",
    title: t("endpoint.title"),
  };

  let models: TimelineItem;
  if (running && !stepDone(deploy, "build")) {
    models = { detail: t("models.building", { locales: enabled }), state: "busy", title: t("models.title") };
  } else if (failed) {
    models = { detail: t("models.failed"), state: "fail", title: t("models.title") };
  } else if (last?.state === "succeeded" || stepDone(deploy, "build")) {
    const built = last?.locales.map((l) => l.locale).join(", ") || enabled;
    models = { detail: t("models.built", { locales: built }), state: "ok", title: t("models.title") };
  } else {
    models = { detail: t("models.notBuilt"), state: "pending", title: t("models.title") };
  }

  let testing: TimelineItem;
  if (running) {
    testing = { detail: t("testing.waiting"), state: "busy", title: t("testing.title") };
  } else if (deploy?.state === "succeeded" || (!failed && last?.state === "succeeded")) {
    const at = deploy?.events.find((e) => e.step === "simulate")?.at ?? last?.at;
    testing = {
      detail: t("testing.passed", { time: formatClock(at) }),
      state: "ok",
      title: t("testing.title"),
    };
  } else if (failed) {
    testing = {
      detail: t("testing.failed"),
      state: "fail",
      title: t("testing.title"),
    };
  } else {
    testing = { detail: t("testing.firstDeploy"), state: "pending", title: t("testing.title") };
  }

  return [amazon, created, endpoint, models, testing];
}

/** Fraction of deploy steps finished, for the progress bar. */
export function deployFraction(deploy: Deploy | undefined): number {
  if (!deploy) return 0;
  const done = new Set(
    deploy.events.filter((e) => e.state === "succeeded" || e.state === "skipped").map((e) => e.step),
  );
  return Math.round((done.size / DEPLOY_STEPS.length) * 100);
}
