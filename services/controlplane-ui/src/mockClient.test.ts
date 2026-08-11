import { describe, expect, it, vi } from "vitest";
import { MockControlPlaneClient } from "./mockClient";

describe("mock control-plane client", () => {
  it("supports local login and session lookup", async () => {
    const client = new MockControlPlaneClient();

    const tokens = await client.login("operator");
    const session = await client.getSession();

    expect(tokens.accessToken).toMatch(/[0-9a-f-]{8,}/);
    expect(session.username).toBe("operator");
  });

  it("creates and deletes DNS records", async () => {
    const client = new MockControlPlaneClient();
    const name = `test-${Date.now()}`;

    const created = await client.upsertDNSRecord(name, { ipv4: "10.20.30.40", ttl: 120 });
    const listed = await client.listDNSRecords();

    expect(created.fqdn).toBe(`${name}.appliance.internal`);
    expect(listed.items.some((record) => record.name === name)).toBe(true);

    await client.deleteDNSRecord(name);

    const afterDelete = await client.listDNSRecords();
    expect(afterDelete.items.some((record) => record.name === name)).toBe(false);
  });

  it("creates API tokens for local API-key workflow development", async () => {
    const client = new MockControlPlaneClient();

    const token = await client.createToken({
      name: `ci-${Date.now()}`,
      scopes: ["artifacts.read"],
      lifetimeSeconds: 3600
    });

    expect(token.token).toMatch(/^mock-/);
    expect(token.scopes).toEqual(["artifacts.read"]);

    const listed = await client.listTokens();
    expect(listed.some((item) => item.id === token.id)).toBe(true);

    await client.deleteToken(token.id);
    const afterRevoke = await client.listTokens();
    expect(afterRevoke.some((item) => item.id === token.id)).toBe(false);
  });

  it("creates a workspace and makes it current", async () => {
    const client = new MockControlPlaneClient();

    const workspace = await client.createWorkspace({
      name: `workspace-${Date.now()}`,
      workProfile: "builder-default"
    });
    const current = await client.getCurrentWorkspace();

    expect(current?.id).toBe(workspace.id);
    expect(current?.workProfile).toBe("builder-default");
  });

  it("simulates build completion", async () => {
    vi.useFakeTimers();
    try {
      const client = new MockControlPlaneClient();

      const job = await client.submitBuild({ targetName: "bundle-controlplane", imageTag: "test" });
      expect(job.status).toBe("running");

      await vi.advanceTimersByTimeAsync(3500);

      const completed = await client.getCurrentBuildStatus();
      expect(completed?.id).toBe(job.id);
      expect(completed?.status).toBe("succeeded");
    } finally {
      vi.useRealTimers();
    }
  });

  it("scans and connects client Wi-Fi networks", async () => {
    const client = new MockControlPlaneClient();

    const scan = await client.scanHostWifi();
    expect(scan.networks?.length).toBeGreaterThan(0);

    const status = await client.applyHostWifi({
      desired: true,
      ssid: "office-lan",
      psk: "long-enough-secret",
      security: "wpa2-psk"
    });

    expect(status.actual).toBe("active");
    expect(status.ssid).toBe("office-lan");
  });

  it("returns the advertised mdns hostname when enabled", async () => {
    const client = new MockControlPlaneClient();

    const status = await client.applyHostMDNS({ desired: true });

    expect(status.actual).toBe("active");
    expect(status.advertisedName).toBe("mock-host.local");
  });
});
