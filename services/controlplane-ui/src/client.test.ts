import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadAuth, saveAuth } from "./auth";
import { ApiError, RemoteControlPlaneClient } from "./client";
import { resetDomStorage } from "./test/domStorage";

describe("remote control-plane client", () => {
  beforeEach(() => {
    resetDomStorage();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("refreshes credentials once after a protected request returns 401", async () => {
    saveAuth({
      accessToken: "old-access",
      refreshToken: "old-refresh",
      accessExpiresAt: "2026-08-03T01:00:00Z"
    });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401, statusText: "Unauthorized" }))
      .mockResolvedValueOnce(
        jsonResponse({
          accessToken: "new-access",
          refreshToken: "new-refresh",
          accessExpiresAt: "2026-08-03T02:00:00Z"
        })
      )
      .mockResolvedValueOnce(jsonResponse({ userId: "u1", username: "admin", domain: "local", authMethod: "password", permissions: [] }));
    vi.stubGlobal("fetch", fetchMock);

    const client = new RemoteControlPlaneClient("https://appliance.example/");
    const session = await client.getSession();

    expect(session.username).toBe("admin");
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[0]?.[1]?.headers.get("Authorization")).toBe("Bearer old-access");
    expect(fetchMock.mock.calls[1]?.[0]).toBe("https://appliance.example/api/v1/auth/refresh");
    expect(fetchMock.mock.calls[2]?.[1]?.headers.get("Authorization")).toBe("Bearer new-access");
    expect(loadAuth()?.refreshToken).toBe("new-refresh");
  });

  it("sends domain local when login domain is omitted or empty", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      jsonResponse({
        accessToken: "access",
        refreshToken: "refresh",
        accessExpiresAt: "2026-08-03T02:00:00Z"
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new RemoteControlPlaneClient("https://appliance.example/");
    await client.login("admin", "secret");
    await client.login("admin", "secret", "");
    await client.login("admin", "secret", "  ");

    expect(fetchMock).toHaveBeenCalledTimes(3);
    for (const call of fetchMock.mock.calls) {
      const init = call[1] as RequestInit;
      const body = JSON.parse(String(init.body)) as { domain: string };
      expect(body.domain).toBe("local");
    }
  });

  it("encodes repository path segments while preserving repository slashes", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: ["latest"] }));
    vi.stubGlobal("fetch", fetchMock);

    const client = new RemoteControlPlaneClient("");
    await client.listRepositoryTags("team/app image");

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "/api/v1/registry/repositories/team/app%20image/tags"
    );
  });

  it("encodes referrer digest query values", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    vi.stubGlobal("fetch", fetchMock);

    const client = new RemoteControlPlaneClient("");
    await client.listRepositoryReferrers("team/app", "sha256:abc/def");

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "/api/v1/registry/repositories/team/app/referrers?digest=sha256%3Aabc%2Fdef"
    );
  });

  it("clears local auth even when logout request fails", async () => {
    saveAuth({
      accessToken: "old-access",
      refreshToken: "old-refresh",
      accessExpiresAt: "2026-08-03T01:00:00Z"
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("logout failed", { status: 500, statusText: "Server Error" }))
    );

    const client = new RemoteControlPlaneClient("");

    await expect(client.logout()).rejects.toMatchObject({ status: 500 });
    expect(loadAuth()).toBeNull();
  });

  it("reports plain text API error bodies without double-reading the response body", async () => {
    const error = await ApiError.fromResponse(
      new Response("plain failure", { status: 503, statusText: "Unavailable" })
    );

    expect(error.status).toBe(503);
    expect(error.detail).toBe("plain failure");
  });

  it("treats empty DELETE responses as success when revoking API tokens", async () => {
    saveAuth({
      accessToken: "access",
      refreshToken: "refresh",
      accessExpiresAt: "2026-08-03T02:00:00Z"
    });
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204, statusText: "No Content" }));
    vi.stubGlobal("fetch", fetchMock);

    const client = new RemoteControlPlaneClient("https://appliance.example/");
    await expect(client.deleteToken("tok-1")).resolves.toBeUndefined();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://appliance.example/api/v1/tokens/tok-1");
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).method).toBe("DELETE");
  });

  it("constructs a remote client when crypto.randomUUID is missing", async () => {
    const original = crypto.randomUUID;
    Object.defineProperty(crypto, "randomUUID", {
      configurable: true,
      writable: true,
      value: undefined
    });
    try {
      vi.resetModules();
      const { createControlPlaneClient } = await import("./client");
      expect(() => createControlPlaneClient()).not.toThrow();
    } finally {
      Object.defineProperty(crypto, "randomUUID", {
        configurable: true,
        writable: true,
        value: original
      });
      vi.resetModules();
    }
  });
});

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}
