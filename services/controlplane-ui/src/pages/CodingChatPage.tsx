import React, { useState } from "react";
import { Button, Card, PageFrame } from "../components";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";

// Coding Chat is deliberately a small appliance-owned launch surface. The
// chat application itself remains behind the session bridge; this page never
// renders, stores, or exposes its one-time grant.
export function CodingChatPage(): React.JSX.Element {
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState("");

  async function openCodingChat() {
    setOpening(true);
    setError("");
    try {
      await client.launchAIWorkspace();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Coding Chat is unavailable.");
      setOpening(false);
    }
  }

  return (
    <PageFrame
      title="Coding Chat"
      eyebrow="Manage"
      description="Use the appliance-hosted chat workspace with the currently loaded model."
      pathname="/manage/coding-chat"
      onNavigate={navigate}
      tabs={[]}
    >
      <div className="stack">
        {error ? <div className="message message--error" role="alert">{error}</div> : null}
        <Card
          title="Start a coding chat"
          subtitle="Your appliance session is used to open a private workspace. A ready model and inference access are required."
        >
          <div className="button-row">
            <Button type="button" onClick={() => void openCodingChat()} disabled={opening}>
              {opening ? "Opening Coding Chat…" : "Open Coding Chat"}
            </Button>
          </div>
          <p className="muted">
            If this is unavailable, ask an appliance administrator to install an LLM pack and load a model, or grant you inference access.
          </p>
        </Card>
      </div>
    </PageFrame>
  );
}
