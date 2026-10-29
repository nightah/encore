import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { keys } from "@api/keys";

/** Which of `cached` an invalidation of `prefix` reaches, the way TanStack Query matches keys. */
function reached(prefix: readonly unknown[], cached: Array<readonly unknown[]>) {
  const client = new QueryClient();
  for (const key of cached) client.setQueryData(key, {});
  void client.invalidateQueries({ queryKey: prefix });
  return cached.filter((key) => client.getQueryState(key)?.isInvalidated);
}

describe("query keys", () => {
  it("refresh each Kodi instance's detail with the instance list", () => {
    expect(reached(keys.instances, [keys.instance(1), keys.instance(2), keys.discovered])).toEqual([
      keys.instance(1),
      keys.instance(2),
    ]);
  });

  it("refresh a library's recent titles and aliases separately from other libraries", () => {
    expect(reached(["libraries", 4], [keys.recent(4, 20), keys.recent(5, 20), keys.aliases(4)])).toEqual([
      keys.recent(4, 20),
    ]);
    expect(reached(keys.libraries, [keys.recent(4, 20), keys.recent(5, 5)])).toHaveLength(2);
  });

  it("keep a running deploy out of the skill's refreshes", () => {
    expect(reached(keys.skill, [keys.deploy("d-1"), keys.authorize])).toEqual([keys.authorize]);
  });

  it("refresh every filtered request list but not request details", () => {
    const lists = [keys.requestList({}), keys.requestList({ outcome: "error" }), keys.requestList({ device: 2 })];
    expect(reached(keys.requests, [...lists, keys.request(7)])).toEqual(lists);
  });

  it("keep reachability checks per address", () => {
    expect(keys.reachability("https://encore.example.net")).not.toEqual(keys.reachability("https://other.example"));
    expect(reached(["setup", "reachability"], [keys.reachability("https://a.example"), keys.auth])).toEqual([
      keys.reachability("https://a.example"),
    ]);
  });
});
