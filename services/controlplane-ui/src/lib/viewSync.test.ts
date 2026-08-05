import { describe, expect, it, beforeEach } from "vitest";
import {
  getLastViewSyncRequest,
  getViewSyncRegionGeneration,
  getViewSyncTagGeneration,
  requestViewSync,
  resetViewSyncForTests,
  withViewSync
} from "./viewSync";

describe("viewSync", () => {
  beforeEach(() => {
    resetViewSyncForTests();
  });

  it("bumps only the requested regions and tags", () => {
    requestViewSync({
      regions: ["shell.alerts", "page"],
      tags: ["licensing"],
      reason: "test"
    });
    expect(getViewSyncRegionGeneration("shell.alerts")).toBe(1);
    expect(getViewSyncRegionGeneration("page")).toBe(1);
    expect(getViewSyncRegionGeneration("shell.bootstrap")).toBe(0);
    expect(getViewSyncTagGeneration("licensing")).toBe(1);
    expect(getLastViewSyncRequest()?.reason).toBe("test");
  });

  it("withViewSync invalidates only after success", async () => {
    await expect(
      withViewSync(async () => {
        throw new Error("fail");
      }, { regions: ["shell.alerts"] })
    ).rejects.toThrow("fail");
    expect(getViewSyncRegionGeneration("shell.alerts")).toBe(0);

    const value = await withViewSync(async () => 42, {
      regions: ["shell.alerts"],
      tags: ["setup"]
    });
    expect(value).toBe(42);
    expect(getViewSyncRegionGeneration("shell.alerts")).toBe(1);
    expect(getViewSyncTagGeneration("setup")).toBe(1);
  });
});
