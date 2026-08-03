import { describe, expect, it } from "vitest";
import { displayProductVersion } from "./productVersion";

describe("displayProductVersion", () => {
  it("prefers semver from product or tagged builds", () => {
    expect(displayProductVersion("0.1.0")).toBe("0.1.0");
    expect(displayProductVersion("v0.1.0")).toBe("0.1.0");
    expect(displayProductVersion("appliance-0.1.0-bundle")).toBe("0.1.0");
    expect(displayProductVersion("0.1.0-dirty")).toBe("0.1.0");
  });

  it("accepts dated product versions", () => {
    expect(displayProductVersion("2026.08.03")).toBe("2026.08.03");
  });

  it("rejects bare git commit identities", () => {
    expect(displayProductVersion("5940c8f")).toBe("");
    expect(displayProductVersion("fc50f5c")).toBe("");
    expect(displayProductVersion("deadbeef")).toBe("");
    expect(displayProductVersion("0123456789abcdef0123456789abcdef01234567")).toBe("");
  });

  it("returns empty for missing values", () => {
    expect(displayProductVersion("")).toBe("");
    expect(displayProductVersion(null)).toBe("");
    expect(displayProductVersion(undefined)).toBe("");
  });
});
