import { describe, expect, it } from "vitest";
import { ensureBlobType, mimeTypeForPath, previewKind } from "./filePreview";

describe("filePreview", () => {
  it("maps pdf paths to application/pdf", () => {
    expect(mimeTypeForPath("docs/sample.pdf")).toBe("application/pdf");
    expect(previewKind("docs/sample.pdf")).toBe("pdf");
  });

  it("re-wraps generic octet-stream blobs with path-derived MIME", () => {
    const raw = new Blob([new Uint8Array([0x25, 0x50, 0x44, 0x46])], { type: "application/octet-stream" });
    const typed = ensureBlobType(raw, "sample.pdf");
    expect(typed.type).toBe("application/pdf");
  });

  it("preserves explicit server content types", () => {
    const raw = new Blob(["hello"], { type: "text/plain" });
    expect(ensureBlobType(raw, "readme.txt").type).toBe("text/plain");
  });
});
