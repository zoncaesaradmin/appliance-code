import { afterEach, describe, expect, it, vi } from "vitest";
import { navigate, requiresDocumentNavigation } from "./navigate";

describe("workspace navigation", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("loads the separate Geo document for Geo routes", () => {
    expect(requiresDocumentNavigation("/geo/login")).toBe(true);
    expect(requiresDocumentNavigation("/geo?view=map")).toBe(true);
    expect(requiresDocumentNavigation("/geography")).toBe(false);
  });

  it("replaces the root document when entering the Geo login", () => {
    const replace = vi.fn();
    vi.stubGlobal("window", { location: { replace, assign: vi.fn() } });
    navigate("/geo/login", true);
    expect(replace).toHaveBeenCalledWith("/geo/login");
  });

  it("keeps console navigation within the current document", () => {
    const replace = vi.spyOn(window.history, "replaceState");
    navigate("/console/login", true);
    expect(replace).toHaveBeenCalledWith({}, "", "/console/login");
    replace.mockRestore();
  });
});
