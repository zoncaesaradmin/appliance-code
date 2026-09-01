import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { randomUUID } from "./randomUUID";

describe("randomUUID", () => {
  let originalRandomUUID: Crypto["randomUUID"] | undefined;

  beforeEach(() => {
    originalRandomUUID = crypto.randomUUID;
  });

  afterEach(() => {
    Object.defineProperty(crypto, "randomUUID", {
      configurable: true,
      writable: true,
      value: originalRandomUUID
    });
  });

  it("returns a UUID when crypto.randomUUID is present", () => {
    expect(randomUUID()).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  it("falls back when crypto.randomUUID is missing (HTTP insecure context)", () => {
    Object.defineProperty(crypto, "randomUUID", {
      configurable: true,
      writable: true,
      value: undefined
    });

    const value = randomUUID();
    expect(value).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });
});
