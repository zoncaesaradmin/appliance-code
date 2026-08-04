import { describe, expect, it } from "vitest";
import {
  MODES,
  currentMode,
  isSystemAdministrator,
  modeUsesFeatureSelector,
  visibleModes
} from "./navigation";

describe("navigation model", () => {
  it("keeps Home as the direct landing page without a feature selector", () => {
    const home = MODES.find((mode) => mode.id === "home");

    expect(home?.defaultPath).toBe("/home");
    expect(home?.features).toHaveLength(0);
    expect(home && modeUsesFeatureSelector(home)).toBe(false);
  });

  it("uses transient feature selectors for Manage, Analyze, and Admin", () => {
    for (const id of ["manage", "analyze", "admin"]) {
      const mode = MODES.find((entry) => entry.id === id);

      expect(mode, `${id} mode should exist`).toBeDefined();
      expect(mode?.features.length).toBeGreaterThan(0);
      expect(mode && modeUsesFeatureSelector(mode)).toBe(true);
    }
  });

  it("maps nested paths to the owning mode", () => {
    expect(currentMode("/manage/dns").id).toBe("manage");
    expect(currentMode("/analyze/workflows").id).toBe("analyze");
    expect(currentMode("/admin/system-status").id).toBe("admin");
    expect(currentMode("/admin/system-status/resources").id).toBe("admin");
    expect(currentMode("/admin/profiles").id).toBe("admin");
    expect(currentMode("/admin/licensing").id).toBe("admin");
    expect(currentMode("/admin/host-services").id).toBe("admin");
  });

  it("falls back to Home for unknown paths", () => {
    expect(currentMode("/does-not-exist").id).toBe("home");
  });

  it("keeps Admin visible for system administrator sessions", () => {
    const modes = visibleModes({
      session: { username: "admin", permissions: [] }
    });

    expect(modes.map((mode) => mode.id)).toContain("admin");
  });

  it("hides Admin for non-administrator sessions", () => {
    const modes = visibleModes({
      session: { username: "developer", permissions: ["builds.create", "artifacts.read"] }
    });

    expect(modes.map((mode) => mode.id)).toEqual(["home", "manage", "analyze"]);
    expect(currentMode("/admin/system-status", modes).id).toBe("home");
  });

  it("recognizes administrator sessions from system permissions", () => {
    expect(
      isSystemAdministrator({
        username: "operator",
        permissions: ["system.operate"]
      })
    ).toBe(true);
  });
});
