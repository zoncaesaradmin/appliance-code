import { describe, expect, it } from "vitest";
import { consolePath, loginDestination, safeNextPath, workspaceForPath } from "./workspace";

describe("workspace routing", () => {
  it("treats the explicit app namespace as application context", () => {
    expect(workspaceForPath("/app")).toBe("app");
    expect(workspaceForPath("/app/discover")).toBe("app");
    expect(workspaceForPath("/")).toBe("console");
  });

  it("maps console paths to the existing shell without changing legacy URLs", () => {
    expect(consolePath("/console")).toBe("/home");
    expect(consolePath("/console/admin/system-status")).toBe("/admin/system-status");
    expect(consolePath("/console/manage/files")).toBe("/manage/files");
    expect(consolePath("/home")).toBe("/home");
  });

  it("rejects external, API, and authentication destinations", () => {
    expect(safeNextPath("//evil.example")).toBe("/home");
    expect(safeNextPath("/api/v1/auth/session")).toBe("/home");
    expect(safeNextPath("/login")).toBe("/home");
    expect(safeNextPath("/console")).toBe("/console");
  });

  it("preserves a direct console or app intent through login", () => {
    expect(loginDestination("/console", "")).toBe("/home");
    expect(loginDestination("/app", "")).toBe("/app");
    expect(loginDestination("/login", "?next=%2Fconsole%2Fadmin%2Fprofiles")).toBe(
      "/console/admin/profiles"
    );
  });
});

