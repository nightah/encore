import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { SkillInfo } from "@api/generated/model";
import { useActiveDeploy } from "@api/skill";

const skillWith = (lastDeploy: SkillInfo["last_deploy"]) => ({ last_deploy: lastDeploy }) as SkillInfo;

describe("useActiveDeploy", () => {
  it("follows a deploy already in progress and keeps it after it ends", () => {
    const running = skillWith({ id: "d-7", state: "in_progress" } as SkillInfo["last_deploy"]);
    const { result, rerender } = renderHook(({ skill }) => useActiveDeploy(skill), {
      initialProps: { skill: running },
    });

    expect(result.current[0]).toBe("d-7");

    rerender({ skill: skillWith({ id: "d-7", state: "succeeded" } as SkillInfo["last_deploy"]) });
    expect(result.current[0]).toBe("d-7");
  });

  it("follows nothing when no deploy is running, until one is started", () => {
    const { result } = renderHook(() =>
      useActiveDeploy(skillWith({ id: "d-6", state: "failed" } as SkillInfo["last_deploy"])),
    );

    expect(result.current[0]).toBeUndefined();

    act(() => result.current[1]("d-8"));
    expect(result.current[0]).toBe("d-8");
  });
});
