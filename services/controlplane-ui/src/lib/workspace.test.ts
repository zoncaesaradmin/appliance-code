import { describe, expect, it } from "vitest";
import {
  consolePath,
  defaultAuthenticatedPath,
  defaultLoginPath,
  isApplicationEntryPath,
  isApplicationPath,
  isApplicationSession,
  isGeoPath,
  loginDestination,
  safeNextPath,
  workspaceForPath
} from "./workspace";

describe("workspace routing", () => {
  it("treats the explicit app namespace as application context", () => {
    expect(workspaceForPath("/app")).toBe("app");
    expect(workspaceForPath("/app/discover")).toBe("app");
    expect(isApplicationPath("/app/login")).toBe(true);
    expect(workspaceForPath("/")).toBe("console");
    expect(workspaceForPath("/", "application-core")).toBe("app");
    expect(workspaceForPath("/geo")).toBe("geo");
    expect(isGeoPath("/geo/login")).toBe(true);
    expect(workspaceForPath("/", "geo-zone")).toBe("geo");
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

  it("uses a code-defined profile policy with a console fallback", () => {
    expect(defaultLoginPath("application-core")).toBe("/app/login");
    expect(defaultAuthenticatedPath("application-core")).toBe("/app");
    expect(defaultLoginPath("geo-zone")).toBe("/geo/login");
    expect(defaultAuthenticatedPath("geo-zone")).toBe("/geo");
    expect(isApplicationEntryPath("/", "application-core")).toBe(true);
    expect(defaultLoginPath("core")).toBe("/console/login");
    expect(defaultAuthenticatedPath("unknown-profile")).toBe("/home");
    expect(isApplicationEntryPath("/", "unknown-profile")).toBe(false);
  });

  it("rejects external, API, and authentication destinations", () => {
    expect(safeNextPath("//evil.example")).toBe("/home");
    expect(safeNextPath("/api/v1/auth/session")).toBe("/home");
    expect(safeNextPath("/login")).toBe("/home");
    expect(safeNextPath("/console/login")).toBe("/home");
    expect(safeNextPath("/app/login")).toBe("/home");
    expect(safeNextPath("/geo/login")).toBe("/home");
    expect(safeNextPath("/console")).toBe("/console");
  });

  it("preserves a direct console or app intent through login", () => {
    expect(loginDestination("/", "")).toBe("/home");
    expect(loginDestination("/", "", "application-core")).toBe("/app");
    expect(loginDestination("/console", "")).toBe("/home");
    expect(loginDestination("/login", "")).toBe("/home");
    expect(loginDestination("/console/login", "")).toBe("/home");
    expect(loginDestination("/app", "")).toBe("/app");
    expect(loginDestination("/app/login", "")).toBe("/app");
    expect(loginDestination("/geo", "")).toBe("/geo");
    expect(loginDestination("/geo/login", "")).toBe("/geo");
    expect(loginDestination("/console/login", "?next=%2Fconsole%2Fadmin%2Fprofiles")).toBe(
      "/console/admin/profiles"
    );
  });
});

