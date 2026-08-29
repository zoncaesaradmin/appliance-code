import { beforeEach, describe, expect, it } from "vitest";
import { clearAuth, isAuthPersisted, loadAuth, saveAuth, setAuthPersist } from "./auth";
import { resetDomStorage } from "./test/domStorage";
import type { LoginResponse } from "./types";

const tokens: LoginResponse = {
  accessToken: "access-token",
  refreshToken: "refresh-token",
  accessExpiresAt: "2026-08-03T01:00:00Z"
};

describe("auth storage", () => {
  beforeEach(() => {
    resetDomStorage();
  });

  it("saves and loads login tokens from session storage by default", () => {
    saveAuth(tokens);

    expect(loadAuth()).toEqual(tokens);
    expect(window.sessionStorage.getItem("appliance.controlplane-ui.auth")).toContain("access-token");
    expect(window.localStorage.getItem("appliance.controlplane-ui.auth")).toBeNull();
  });

  it("persists login tokens in local storage when remember-me is on", () => {
    setAuthPersist(true);
    saveAuth(tokens);

    expect(isAuthPersisted()).toBe(true);
    expect(loadAuth()).toEqual(tokens);
    expect(window.localStorage.getItem("appliance.controlplane-ui.auth")).toContain("access-token");
    expect(window.sessionStorage.getItem("appliance.controlplane-ui.auth")).toBeNull();
  });

  it("keeps the persist preference after logout so the next sign-in can reuse it", () => {
    setAuthPersist(true);
    saveAuth(tokens);
    clearAuth();

    expect(loadAuth()).toBeNull();
    expect(isAuthPersisted()).toBe(true);
  });

  it("clears tokens", () => {
    saveAuth(tokens);
    clearAuth();
    expect(loadAuth()).toBeNull();
  });

  it("drops corrupt storage instead of reusing invalid auth state", () => {
    window.sessionStorage.setItem("appliance.controlplane-ui.auth", "{not-json");

    expect(loadAuth()).toBeNull();
    expect(window.sessionStorage.getItem("appliance.controlplane-ui.auth")).toBeNull();
  });
});
