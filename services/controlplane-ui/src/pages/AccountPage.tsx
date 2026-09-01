import React, { FormEvent, useEffect, useState } from "react";
import { Card, EmptyState, PageFrame } from "../components";
import { client } from "../lib/api";
import { formatTimestamp } from "../lib/format";
import { navigate } from "../lib/navigate";
import type { APIToken, CreateTokenResponse, Session } from "../types";

function activeTokens(tokens: APIToken[]): APIToken[] {
  return tokens.filter((token) => !token.revokedAt);
}

export function AccountPage(props: {
  pathname: string;
  session: Session;
  onSignedOut: () => void;
}): React.JSX.Element {
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [createdToken, setCreatedToken] = useState<CreateTokenResponse | null>(null);
  const [message, setMessage] = useState("");
  const [messageTone, setMessageTone] = useState<"ok" | "error">("ok");
  const [creating, setCreating] = useState(false);
  const [revokingID, setRevokingID] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
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
    void client
      .listTokens()
      .then((items) => setTokens(activeTokens(items)))
      .catch(() => setTokens([]));
  }, [props.pathname]);

  async function createToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = tokenName.trim();
    if (!name) {
      setMessageTone("error");
      setMessage("Token name is required.");
      return;
    }
    setMessage("");
    setCopied(false);
    setCreating(true);
    try {
      // Omit scopes so the token inherits this user's full permissions.
      // Artifact-only scopes break seed-build-deps: OCI needs artifacts.*,
      // while /api/v1/files uploads need files.write.
      const response = await client.createToken({
        name,
        lifetimeSeconds: 90 * 24 * 60 * 60
      });
      setCreatedToken(response);
      setTokenName("");
      setTokens(activeTokens(await client.listTokens()));
      setMessageTone("ok");
      setMessage("API token created. Copy the secret now; it is only shown once.");
    } catch (error) {
      setMessageTone("error");
      setMessage(error instanceof Error ? error.message : "Could not create the API token.");
    } finally {
      setCreating(false);
    }
  }

  async function revokeToken(id: string) {
    setMessage("");
    setRevokingID(id);
    try {
      await client.deleteToken(id);
      if (createdToken?.id === id) {
        setCreatedToken(null);
        setCopied(false);
      }
      setTokens(activeTokens(await client.listTokens()));
      setMessageTone("ok");
      setMessage("API token revoked.");
    } catch (error) {
      setMessageTone("error");
      setMessage(error instanceof Error ? error.message : "Could not revoke the API token.");
    } finally {
      setRevokingID(null);
    }
  }

  async function copySecret() {
    if (!createdToken?.token) {
      return;
    }
    try {
      await navigator.clipboard.writeText(createdToken.token);
      setCopied(true);
    } catch {
      setMessageTone("error");
      setMessage("Could not copy the secret. Select and copy it manually.");
    }
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
        <div className="stack">
          <Card
            title="Artifact server access"
            subtitle="Use these API tokens as the password when logging registry clients into the appliance artifact server"
          >
            <p className="m-0 text-sm leading-6 text-slate-600">
              Clients such as Podman, Skopeo, Helm, and ORAS authenticate with your appliance username and an
              API token (never your interactive password). Registry path permissions still come from Artifacts →
              Grants.
            </p>
            <div className="button-row mt-3">
              <button className="button button--ghost" type="button" onClick={() => navigate("/manage/artifacts/grants")}>
                Open registry grants
              </button>
            </div>
          </Card>
          <div className="grid-two">
            <Card title="Create API token" subtitle="Shown once at creation; 90-day lifetime; inherits your permissions">
              <form className="stack-form" onSubmit={(event) => void createToken(event)}>
                <label className="field">
                  <span>Token name</span>
                  <input
                    value={tokenName}
                    onChange={(event) => setTokenName(event.target.value)}
                    placeholder="e.g. build-host-seed"
                    required
                    autoComplete="off"
                  />
                </label>
                <p className="m-0 text-sm leading-6 text-slate-600">
                  Use this token as <code>DEV_REGISTRY_TOKEN</code> for registry login and file API uploads
                  (<code>make seed-build-deps</code>, publish). It covers both OCI (<code>/v2</code>) and files
                  (<code>/api/v1/files</code>).
                </p>
                <button
                  className="button button--primary"
                  type="submit"
                  disabled={creating || !tokenName.trim()}
                >
                  {creating ? "Creating..." : "Create token"}
                </button>
              </form>
              {message ? (
                <div className={messageTone === "error" ? "message message--error" : "message"}>{message}</div>
              ) : null}
              {createdToken ? (
                <div className="secret-panel">
                  <strong>New secret</strong>
                  <code>{createdToken.token}</code>
                  <div className="button-row">
                    <button className="button button--primary" type="button" onClick={() => void copySecret()}>
                      {copied ? "Copied" : "Copy secret"}
                    </button>
                  </div>
                </div>
              ) : null}
            </Card>
            <Card title="Current API tokens" subtitle="Active tokens for this account">
              {tokens.length === 0 ? (
                <EmptyState message="No active API tokens yet. Create one to access the artifact server from a client." />
              ) : (
                <div className="table-list">
                  {tokens.map((token) => (
                    <div className="table-list__row" key={token.id}>
                      <div>
                        <strong>{token.name}</strong>
                        <span>Expires {formatTimestamp(token.expiresAt)}</span>
                      </div>
                      <button
                        className="button button--ghost"
                        type="button"
                        disabled={revokingID === token.id}
                        onClick={() => void revokeToken(token.id)}
                      >
                        {revokingID === token.id ? "Revoking..." : "Revoke"}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </div>
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
                <span>Display name</span>
                <strong>{props.session.displayName || props.session.username}</strong>
              </div>
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
