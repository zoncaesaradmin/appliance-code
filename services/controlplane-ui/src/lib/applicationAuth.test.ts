import { afterEach, describe, expect, it, vi } from "vitest";
import { applicationAuth, ApplicationAuthError, presentedAuthError, requiresSignIn } from "./applicationAuth";

function jsonResponse(status: number, body: unknown, statusText = ""): Response {
  return new Response(JSON.stringify(body), {
    status,
    statusText,
    headers: { "Content-Type": "application/json" }
  });
}

describe("application auth errors", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("hides HTTP status text when the sign-in service is missing", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("missing", { status: 404, statusText: "Not Found" })));

    await expect(applicationAuth.challenge("+919876543210")).rejects.toMatchObject({
      name: "ApplicationAuthError",
      status: 404,
      message: "Could not send a code. Try again."
    });
  });

  it("hides transport failures that are not written for a member", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(500, { message: "sql: connection refused" }, "Internal Server Error"))
    );

    await expect(applicationAuth.verify("challenge", "123456")).rejects.toMatchObject({
      message: "Could not verify the code. Try again."
    });
  });

  it("keeps a member sentence from the identity service", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse(401, { message: "The code is invalid or expired." }, "Unauthorized"))
    );

    await expect(applicationAuth.verify("challenge", "000000")).rejects.toMatchObject({
      status: 401,
      message: "The code is invalid or expired."
    });
  });

  it("uses the retry sentence when the browser cannot reach the service", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    await expect(applicationAuth.challenge("+919876543210")).rejects.toMatchObject({
      status: 0,
      message: "Could not send a code. Try again."
    });
  });

  it("does not present an unexpected exception as page copy", () => {
    expect(presentedAuthError(new Error("Not Found"), "Could not send a code. Try again.")).toBe(
      "Could not send a code. Try again."
    );
    expect(presentedAuthError(new ApplicationAuthError(429, "Too many attempts. Try again later."), "Could not send a code. Try again.")).toBe(
      "Too many attempts. Try again later."
    );
  });

  it("treats an expired application session as a sign-in redirect", () => {
    expect(requiresSignIn(new ApplicationAuthError(401, "Sign in first."))).toBe(true);
    expect(requiresSignIn(new ApplicationAuthError(404, "Could not submit the join request."))).toBe(false);
    expect(requiresSignIn(new Error("Sign in first."))).toBe(false);
  });
});
