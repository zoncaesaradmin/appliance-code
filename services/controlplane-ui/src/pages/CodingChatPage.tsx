import React, { useState } from "react";
import { Button, Card, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";

// AI Chat is deliberately a small appliance-owned launch surface. The
// chat application itself remains behind the session bridge; this page never
// renders, stores, or exposes its one-time grant.
export function CodingChatPage(props: { capabilities: string[] }): React.JSX.Element {
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState("");
  const webUIEnabled = props.capabilities.includes("open-webui");

  async function openCodingChat() {
    setOpening(true);
    setError("");
    try {
      await client.launchAIWorkspace();
    } catch (err) {
      setError(err instanceof Error ? err.message : "AI Chat is unavailable.");
      setOpening(false);
    }
  }

  return (
    <PageFrame
      title="AI Chat"
      eyebrow="Manage"
      description="Use the appliance-hosted chat workspace with the currently loaded model."
      pathname="/manage/ai-chat"
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="stack">
        {error ? <div className="message message--error" role="alert">{error}</div> : null}
        {webUIEnabled ? (
          <Card
            title="Start a chat"
            subtitle="Your appliance session is used to open a private workspace. A ready model and inference access are required."
          >
            <div className="button-row">
              <Button type="button" onClick={() => void openCodingChat()} disabled={opening}>
                {opening ? "Opening AI Chat…" : "Open AI Chat"}
              </Button>
            </div>
            <p className="muted">
              If this is unavailable, ask an appliance administrator to install the open-webui pack and load a
              model, or grant you inference access.
            </p>
          </Card>
        ) : (
          <Card
            title="Web UI is not enabled in this profile"
            subtitle="AI Chat needs the open-webui capability. This appliance profile provides inference without the optional chat Web UI."
          >
            <p className="muted" role="status">
              Use an open-webui profile such as private-ai-webui, or ask an administrator to change the
              appliance profile and install the open-webui pack, if chat is required.
            </p>
          </Card>
        )}
      </div>
    </PageFrame>
  );
}
