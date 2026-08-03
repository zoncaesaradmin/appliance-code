/** True when value looks like a bare git short/long SHA, not a product version. */
function looksLikeGitCommit(value: string): boolean {
  return /^[0-9a-f]{7,40}$/i.test(value);
}

/**
 * Product/bundle version for operator-facing display.
 * Prefers X.Y.Z (or dated YYYY.MM.DD); rejects bare commit SHAs so login/setup
 * never shows git identity as the appliance version.
 */
export function displayProductVersion(raw?: string | null): string {
  const value = (raw ?? "").trim();
  if (!value) {
    return "";
  }
  const semver =
    value.match(/(?:^|[^0-9A-Za-z])v?(\d+\.\d+\.\d+)\b/) ??
    value.match(/^v?(\d+\.\d+\.\d+)\b/);
  if (semver) {
    return semver[1];
  }
  const dated = value.match(/(\d{4}\.\d{2}\.\d{2})/);
  if (dated) {
    return dated[1];
  }
  const cleaned = value.replace(/-dirty$/i, "").replace(/\+.*$/, "");
  if (!cleaned || looksLikeGitCommit(cleaned)) {
    return "";
  }
  return cleaned;
}
