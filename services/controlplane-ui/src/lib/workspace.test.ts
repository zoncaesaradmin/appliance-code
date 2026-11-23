import { describe, expect, it } from "vitest";
import {
  consolePath,
  isApplicationPath,
  isApplicationSession,
  loginDestination,
  safeNextPath,
  workspaceForPath
} from "./workspace";

describe("workspace routing", () => {
  it("treats the explicit app namespace as application context", () => {
    expect(workspaceForPath("/app")).toBe("app");
    expect(workspaceForPath("/app/discover")).toBe("app");
    expect(isApplicationPath("/app/login")).toBe(true);
    expect(workspaceForPath("/")).toBe("app");
  });

  it("does not treat an operator session as an application session", () => {
    expect(isApplicationSession({ domain: "local", authMethod: "password" })).toBe(false);
    expect(isApplicationSession({ domain: "local", authMethod: "session" })).toBe(false);
    expect(isApplicationSession({ domain: "application", authMethod: "mobile_otp" })).toBe(true);
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
    expect(safeNextPath("/app/login")).toBe("/home");
    expect(safeNextPath("/console")).toBe("/console");
  });

  it("preserves a direct console or app intent through login", () => {
    expect(loginDestination("/", "")).toBe("/app");
    expect(loginDestination("/console", "")).toBe("/home");
    expect(loginDestination("/login", "")).toBe("/home");
    expect(loginDestination("/app", "")).toBe("/app");
    expect(loginDestination("/app/login", "")).toBe("/app");
    expect(loginDestination("/login", "?next=%2Fconsole%2Fadmin%2Fprofiles")).toBe(
      "/console/admin/profiles"
    );
  });
});

