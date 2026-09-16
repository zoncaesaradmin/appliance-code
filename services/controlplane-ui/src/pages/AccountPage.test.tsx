import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AccountPage } from "./AccountPage";

const api = vi.hoisted(() => ({
  listTokens: vi.fn(),
  createToken: vi.fn(),
  deleteToken: vi.fn(),
  downloadApplianceCA: vi.fn(),
  changePassword: vi.fn()
}));
vi.mock("../lib/api", () => ({ client: api }));
vi.mock("../lib/navigate", () => ({ navigate: vi.fn() }));
vi.mock("../components", () => ({
  PageFrame: ({ children }: { children: React.ReactNode }) => <main>{children}</main>,
  Card: ({ title, children }: { title: string; children: React.ReactNode }) => (
    <section>
      <h2>{title}</h2>
      {children}
    </section>
  ),
  EmptyState: ({ message }: { message: string }) => <p>{message}</p>
}));

const session = {
  userId: "u1",
  username: "admin",
  displayName: "Admin",
  domain: "local",
  authMethod: "password",
  permissions: ["tokens.read.self", "tokens.create.self", "tokens.revoke.self"]
};

let element: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  element = document.createElement("div");
  document.body.append(element);
  root = createRoot(element);
  api.listTokens.mockResolvedValue([]);
  api.downloadApplianceCA.mockResolvedValue(new Blob(["pem"], { type: "application/x-pem-file" }));
});

afterEach(() => {
  act(() => root.unmount());
  element.remove();
});

it("places the CA download card before artifact access on API Keys", async () => {
  await act(async () => {
    root.render(
      <AccountPage pathname="/account/api-keys" session={session as never} onSignedOut={() => undefined} />
    );
  });
  const titles = [...element.querySelectorAll("h2")].map((node) => node.textContent);
  expect(titles[0]).toBe("Appliance CA certificate");
  expect(titles.at(-1)).toBe("Artifact server access");
  expect(titles).toContain("Create API token");
  expect(titles).toContain("Current API tokens");
});

it("downloads appliance-ca.pem from the CA card", async () => {
  const click = vi.fn();
  const createElement = document.createElement.bind(document);
  vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
    const node = createElement(tag);
    if (tag === "a") {
      Object.defineProperty(node, "click", { value: click });
    }
    return node;
  });
  const revoke = vi.fn();
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: () => "blob:ca",
    revokeObjectURL: revoke
  });

  await act(async () => {
    root.render(
      <AccountPage pathname="/account/api-keys" session={session as never} onSignedOut={() => undefined} />
    );
  });
  const button = [...element.querySelectorAll("button")].find(
    (node) => node.textContent === "Download CA certificate"
  );
  expect(button).toBeTruthy();
  await act(async () => {
    button!.click();
  });
  expect(api.downloadApplianceCA).toHaveBeenCalledTimes(1);
  expect(click).toHaveBeenCalled();
  expect(element.textContent).toContain("Downloaded appliance-ca.pem");
});
