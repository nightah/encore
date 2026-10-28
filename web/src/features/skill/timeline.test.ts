import { describe, expect, it } from "vitest";

import type { Deploy, DeployEvent, SkillInfo } from "@api/generated/model";
import { DEPLOY_STEPS, buildTimeline, deployFraction, shortId } from "@features/skill/timeline";

const at = (hour: number, minute: number) => new Date(2026, 9, 28, hour, minute).toISOString();

function skill(patch: Partial<SkillInfo> = {}): SkillInfo {
  return {
    amazon: {
      connected: true,
      has_client_secret: true,
      needs_reauthorise: false,
      profile_client_id: "x",
      vendor_id: "",
    },
    endpoint: { url: "https://encore.example.net/alexa" },
    last_deploy: null,
    locales_enabled: ["en-AU", "en-GB"],
    skill_id: "",
    ...patch,
  } as SkillInfo;
}

const lastDeploy = (state: "succeeded" | "failed" | "in_progress", locales: string[] = []) =>
  ({
    at: at(20, 41),
    id: "d-1",
    locales: locales.map((locale) => ({ locale })),
    state,
  }) as unknown as SkillInfo["last_deploy"];

const event = (step: DeployEvent["step"], state: DeployEvent["state"], time = at(9, 0)): DeployEvent => ({
  at: time,
  locales: {},
  message: "",
  state,
  step,
});

const deploy = (state: Deploy["state"], events: DeployEvent[] = []): Deploy => ({
  error: "",
  events,
  id: "d-2",
  state,
});

/** The timeline as "title: state (detail)" lines, which is what the page shows. */
const lines = (s: SkillInfo, d?: Deploy) => buildTimeline(s, d).map((i) => `${i.title}: ${i.state} (${i.detail})`);

describe("shortId", () => {
  it("keeps the prefix and both ends of a long id", () => {
    expect(shortId("amzn1.ask.skill.3f2a1b2c-0000-4444-8888-a1b2c3d491c0")).toBe("amzn1.ask.skill.3f2a…91c0");
    expect(shortId("amzn1.ask.skill.3f2a1b2c-0000", 2)).toBe("amzn1.ask.skill.3f…00");
  });

  it("leaves ids short enough to read", () => {
    expect(shortId("amzn1.ask.skill.123456789")).toBe("amzn1.ask.skill.123456789");
    expect(shortId("abcdefghijk")).toBe("abcd…hijk");
    expect(shortId("")).toBe("");
  });
});

describe("buildTimeline", () => {
  it("shows a skill that was never deployed", () => {
    expect(lines(skill({ amazon: { ...skill().amazon, connected: false } }))).toEqual([
      "Amazon account: fail (Not connected)",
      "Skill created: pending (Created on the first deploy)",
      "Endpoint: ok (encore.example.net/alexa)",
      "Models: pending (Not built yet)",
      "Enabled for testing: pending (After the first deploy)",
    ]);
  });

  it("shows a working skill after a successful deploy", () => {
    const s = skill({
      amazon: { ...skill().amazon, vendor_id: "M2ABCDEFXYZ" },
      last_deploy: lastDeploy("succeeded", ["en-AU"]),
      skill_id: "amzn1.ask.skill.3f2a1b2c-0000-4444-8888-a1b2c3d491c0",
    });
    expect(lines(s)).toEqual([
      "Amazon account: ok (Vendor M2AB…YZ · connected)",
      "Skill created: ok (amzn1.ask.skill.3f2a…91c0)",
      "Endpoint: ok (encore.example.net/alexa)",
      "Models: ok (en-AU built)",
      "Enabled for testing: ok (Smoke test passed 20:41)",
    ]);
  });

  it("names the enabled locales as built when the deploy didn't list them", () => {
    expect(lines(skill({ last_deploy: lastDeploy("succeeded") }))[3]).toBe("Models: ok (en-AU, en-GB built)");
  });

  it("asks to re-authorise Amazon when its tokens stopped working", () => {
    const s = skill({ amazon: { ...skill().amazon, needs_reauthorise: true, vendor_id: "M2ABCDEF" } });
    expect(lines(s)[0]).toBe("Amazon account: fail (Re-authorise to keep deploying)");
  });

  it("says connected without a vendor id, and keeps a short vendor id whole", () => {
    expect(lines(skill())[0]).toBe("Amazon account: ok (Connected)");
    expect(lines(skill({ amazon: { ...skill().amazon, vendor_id: "M2ABCD" } }))[0]).toBe(
      "Amazon account: ok (Vendor M2ABCD · connected)",
    );
  });

  it("follows a first deploy while it runs", () => {
    const running = deploy("in_progress", [event("package", "succeeded")]);
    expect(lines(skill(), running).slice(1)).toEqual([
      "Skill created: busy (Creating…)",
      "Endpoint: ok (encore.example.net/alexa)",
      "Models: busy (Building en-AU, en-GB…)",
      "Enabled for testing: busy (Waiting for the smoke test)",
    ]);
  });

  it("shows the models built once the build step is done, while the deploy goes on", () => {
    const running = deploy("in_progress", [event("build", "succeeded")]);
    expect(lines(skill(), running)[3]).toBe("Models: ok (en-AU, en-GB built)");
    expect(lines(skill(), deploy("in_progress", [event("build", "skipped")]))[3]).toBe(
      "Models: ok (en-AU, en-GB built)",
    );
  });

  it("shows a failed deploy, even when an earlier one succeeded", () => {
    const s = skill({ last_deploy: lastDeploy("succeeded", ["en-AU"]), skill_id: "amzn1.ask.skill.1" });
    expect(lines(s, deploy("failed", [event("build", "failed")])).slice(3)).toEqual([
      "Models: fail (The last build failed)",
      "Enabled for testing: fail (Not enabled: fix the failed step and redeploy)",
    ]);
  });

  it("shows the last deploy's failure when no deploy is being followed", () => {
    expect(lines(skill({ last_deploy: lastDeploy("failed") })).slice(3)).toEqual([
      "Models: fail (The last build failed)",
      "Enabled for testing: fail (Not enabled: fix the failed step and redeploy)",
    ]);
  });

  it("times the smoke test from the followed deploy", () => {
    const done = deploy("succeeded", [event("build", "succeeded"), event("simulate", "succeeded", at(7, 5))]);
    expect(lines(skill({ last_deploy: lastDeploy("failed") }), done)[4]).toBe(
      "Enabled for testing: ok (Smoke test passed 07:05)",
    );
  });
});

describe("deployFraction", () => {
  it("is the share of steps finished, counting skipped ones and each step once", () => {
    expect(deployFraction(undefined)).toBe(0);
    expect(deployFraction(deploy("in_progress"))).toBe(0);
    expect(
      deployFraction(
        deploy("in_progress", [
          event("package", "started"),
          event("package", "succeeded"),
          event("vendor", "skipped"),
          event("skill", "failed"),
          event("vendor", "succeeded"),
        ]),
      ),
    ).toBe(25);
    expect(deployFraction(deploy("in_progress", [event("vendor", "skipped"), event("skill", "skipped")]))).toBe(25);
    expect(
      deployFraction(
        deploy(
          "succeeded",
          DEPLOY_STEPS.map((s) => event(s, "succeeded")),
        ),
      ),
    ).toBe(100);
  });
});
