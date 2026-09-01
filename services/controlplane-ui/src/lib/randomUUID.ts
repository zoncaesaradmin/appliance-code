/**
 * UUID v4 that works in insecure browsing contexts.
 *
 * `crypto.randomUUID` is a secure-context API, so it is missing on
 * plaintext HTTP to a LAN IP (for example http://192.168.1.151/).
 * `crypto.getRandomValues` remains available there and is used as fallback.
 */
export function randomUUID(): string {
  const webCrypto = globalThis.crypto;
  if (webCrypto && typeof webCrypto.randomUUID === "function") {
    return webCrypto.randomUUID();
  }
  if (!webCrypto || typeof webCrypto.getRandomValues !== "function") {
    throw new Error("crypto.getRandomValues is unavailable");
  }
  const bytes = new Uint8Array(16);
  webCrypto.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
