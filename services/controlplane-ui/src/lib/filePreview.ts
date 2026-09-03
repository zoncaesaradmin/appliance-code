/** MIME type for a logical file path; used when the server returns octet-stream. */
export function mimeTypeForPath(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  switch (ext) {
    case "pdf":
      return "application/pdf";
    case "txt":
      return "text/plain";
    case "md":
      return "text/markdown";
    case "json":
      return "application/json";
    case "yaml":
    case "yml":
      return "application/yaml";
    case "xml":
      return "application/xml";
    case "csv":
      return "text/csv";
    case "log":
      return "text/plain";
    case "png":
      return "image/png";
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "gif":
      return "image/gif";
    case "webp":
      return "image/webp";
    case "svg":
      return "image/svg+xml";
    default:
      return "application/octet-stream";
  }
}

export type FilePreviewKind = "pdf" | "text" | "image" | "other";

export function previewKind(path: string): FilePreviewKind {
  if (/\.pdf$/i.test(path)) return "pdf";
  if (/\.(txt|md|json|yaml|yml|log|csv|xml)$/i.test(path)) return "text";
  if (/\.(png|jpe?g|gif|webp|svg)$/i.test(path)) return "image";
  return "other";
}

export function previewLabel(path: string): string {
  const kind = previewKind(path);
  if (kind === "pdf") return "PDF";
  if (kind === "text") return "TEXT";
  if (kind === "image") return "IMAGE";
  const ext = path.split(".").at(-1);
  return ext ? ext.toUpperCase() : "FILE";
}

/** Re-wrap a blob with the path-derived MIME when the response type is missing or generic. */
export function ensureBlobType(blob: Blob, path: string): Blob {
  const expected = mimeTypeForPath(path);
  if (blob.type && blob.type !== "application/octet-stream") {
    return blob;
  }
  return new Blob([blob], { type: expected });
}

export function isTextPreview(path: string, blob: Blob): boolean {
  return previewKind(path) === "text" || blob.type.startsWith("text/");
}
