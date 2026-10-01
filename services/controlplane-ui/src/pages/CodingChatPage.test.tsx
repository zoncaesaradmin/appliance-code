import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CodingChatPage } from "./CodingChatPage";

const api = vi.hoisted(() => ({
  launchAIWorkspace: vi.fn()
}));
vi.mock("../lib/api", () => ({ client: api }));
vi.mock("../lib/navigate", () => ({ navigate: vi.fn() }));
vi.mock("../components", () => ({
  PageFrame: ({ children }: { children: React.ReactNode }) => <main>{children}</main>,
  Card: ({
    title,
    subtitle,
    children
  }: {
    title: string;
    subtitle?: string;
    children: React.ReactNode;
  }) => (
    <section>
      <h2>{title}</h2>
      {subtitle ? <p>{subtitle}</p> : null}
      {children}
    </section>
  ),
  Button: ({
    children,
    onClick,
    disabled
  }: {
    children: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  )
}));

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  api.launchAIWorkspace.mockReset();
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

it("explains when open-webui is not enabled in the profile", () => {
  act(() => {
    root.render(<CodingChatPage capabilities={["inference"]} />);
  });
  expect(container.textContent).toContain("Web UI is not enabled in this profile");
  expect(container.textContent).not.toContain("Open AI Chat");
  expect(api.launchAIWorkspace).not.toHaveBeenCalled();
});

it("offers Open AI Chat when open-webui is enabled", () => {
  act(() => {
    root.render(<CodingChatPage capabilities={["inference", "open-webui"]} />);
  });
  expect(container.textContent).toContain("Open AI Chat");
  expect(container.textContent).not.toContain("Web UI is not enabled in this profile");
});
