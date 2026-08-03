import { beforeEach, describe, expect, it } from "vitest";
import { clearAuth, loadAuth, saveAuth } from "./auth";
import type { LoginResponse } from "./types";

describe("auth storage", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
  });

  it("saves and loads login tokens from session storage", () => {
    const tokens: LoginResponse = {
      accessToken: "access-token",
      refreshToken: "refresh-token",
      accessExpiresAt: "2026-08-03T01:00:00Z"
    };

    saveAuth(tokens);

    expect(loadAuth()).toEqual(tokens);
  });

  it("clears tokens", () => {
    saveAuth({
      accessToken: "access-token",
      refreshToken: "refresh-token",
      accessExpiresAt: "2026-08-03T01:00:00Z"
    });

    clearAuth();

    expect(loadAuth()).toBeNull();
  });

  it("drops corrupt storage instead of reusing invalid auth state", () => {
    window.sessionStorage.setItem("appliance.controlplane-ui.auth", "{not-json");

    expect(loadAuth()).toBeNull();
    expect(window.sessionStorage.getItem("appliance.controlplane-ui.auth")).toBeNull();
  });
});
