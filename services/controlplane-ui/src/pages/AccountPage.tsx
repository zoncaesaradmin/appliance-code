import React, { FormEvent, useEffect, useState } from "react";
import { Card, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { APIToken, CreateTokenResponse, Session } from "../types";

export function AccountPage(props: {
  pathname: string;
  session: Session;
  onSignedOut: () => void;
}): React.JSX.Element {
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [createdToken, setCreatedToken] = useState<CreateTokenResponse | null>(null);
  const [message, setMessage] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordMessage, setPasswordMessage] = useState("");
  const [passwordError, setPasswordError] = useState("");
  const [passwordSubmitting, setPasswordSubmitting] = useState(false);

  useEffect(() => {
    if (props.pathname !== "/account/api-keys") {
      return;
    }
    void client.listTokens().then(setTokens).catch(() => setTokens([]));
  }, [props.pathname]);

  async function createToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setMessage("");
    try {
      const response = await client.createToken({
        name: tokenName,
        lifetimeSeconds: 90 * 24 * 60 * 60,
        scopes: ["artifacts.read", "artifacts.write"]
      });
      setCreatedToken(response);
      setTokenName("");
      setTokens(await client.listTokens());
      setMessage("API token created. Copy the secret now; it is only shown once.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Could not create the API token.");
    }
  }

  async function revokeToken(id: string) {
    await client.deleteToken(id);
    setTokens(await client.listTokens());
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPasswordError("");
    setPasswordMessage("");
    if (newPassword !== confirmPassword) {
      setPasswordError("New password and confirmation do not match.");
      return;
    }
    setPasswordSubmitting(true);
    try {
      await client.changePassword(currentPassword, newPassword);
      setPasswordMessage("Password changed. Sign in again with your new password.");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      props.onSignedOut();
    } catch (error) {
      setPasswordError(error instanceof Error ? error.message : "Could not change the password.");
    } finally {
      setPasswordSubmitting(false);
    }
  }

  return (
    <PageFrame
      title={props.session.username}
      eyebrow="Account"
      pathname={props.pathname}
      onNavigate={navigate}
      tabs={[
        { label: "Profile", path: "/account/profile" },
        { label: "Session", path: "/account/session" },
        { label: "Password", path: "/account/password" },
        { label: "API Keys", path: "/account/api-keys" }
      ]}
    >
      {props.pathname === "/account/api-keys" ? (
        <div className="grid-two">
          <Card title="Create API token" subtitle="User-scoped token creation">
            <form className="stack-form" onSubmit={createToken}>
              <label className="field">
                <span>Token name</span>
                <input value={tokenName} onChange={(event) => setTokenName(event.target.value)} />
              </label>
              <button className="button button--primary" type="submit">
                Create token
              </button>
            </form>
            {message ? <div className="message">{message}</div> : null}
            {createdToken ? (
              <div className="secret-panel">
                <strong>New secret</strong>
                <code>{createdToken.token}</code>
              </div>
            ) : null}
          </Card>
          <Card title="Current API tokens" subtitle="Existing token metadata">
            <div className="table-list">
              {tokens.map((token) => (
                <div className="table-list__row" key={token.id}>
                  <div>
                    <strong>{token.name}</strong>
                    <span>Expires {formatTimestamp(token.expiresAt)}</span>
                  </div>
                  <button className="button button--ghost" onClick={() => void revokeToken(token.id)}>
                    Revoke
                  </button>
                </div>
              ))}
            </div>
          </Card>
        </div>
      ) : props.pathname === "/account/password" ? (
        <Card title="Change password" subtitle="Update your local appliance password">
          <form className="stack-form" onSubmit={(event) => void changePassword(event)}>
            <label className="field">
              <span>Current password</span>
              <input
                type="password"
                name="currentPassword"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
                required
              />
            </label>
            <label className="field">
              <span>New password</span>
              <input
                type="password"
                name="newPassword"
                autoComplete="new-password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                required
              />
            </label>
            <label className="field">
              <span>Confirm new password</span>
              <input
                type="password"
                name="confirmPassword"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
                required
              />
            </label>
            {passwordError ? <div className="message message--error">{passwordError}</div> : null}
            {passwordMessage ? <div className="message">{passwordMessage}</div> : null}
            <button className="button button--primary" type="submit" disabled={passwordSubmitting}>
              {passwordSubmitting ? "Updating..." : "Update password"}
            </button>
          </form>
        </Card>
      ) : props.pathname === "/account/session" ? (
        <Card title="Session information" subtitle="Details for the current interactive sign-in">
          <div className="detail-list">
            <div>
              <span>Username</span>
              <strong>{props.session.username}</strong>
            </div>
            <div>
              <span>Auth method</span>
              <strong>{props.session.authMethod}</strong>
            </div>
            <div>
              <span>Domain</span>
              <strong>{props.session.domain}</strong>
            </div>
          </div>
        </Card>
      ) : (
        <div className="grid-two">
          <Card title="Profile" subtitle="Your appliance user identity">
            <div className="detail-list">
              <div>
                <span>Username</span>
                <strong>{props.session.username}</strong>
              </div>
              <div>
                <span>User ID</span>
                <strong>{props.session.userId}</strong>
              </div>
              <div>
                <span>Domain</span>
                <strong>{props.session.domain}</strong>
              </div>
            </div>
          </Card>
          <Card title="Permissions" subtitle="Resolved control-plane permissions">
            <div className="badge-row">
              {props.session.permissions.map((permission) => (
                <span className="pill" key={permission}>
                  {permission}
                </span>
              ))}
            </div>
          </Card>
        </div>
      )}
    </PageFrame>
  );
}
