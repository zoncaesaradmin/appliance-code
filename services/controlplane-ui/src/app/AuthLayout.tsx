import React, { FormEvent, useEffect, useState } from "react";
import { isAuthPersisted } from "../auth";
import { BrandMark } from "../components";
import { client } from "../lib/api";
import { displayProductVersion } from "../productVersion";

export function AuthLayout(props: {
  mode: "login" | "setup";
  onLogin: (username: string, password: string, persist: boolean) => Promise<void>;
  onSetup: (username: string, password: string, persist: boolean) => Promise<void>;
  guestAccess: boolean;
  onGuest: (name: string, persist: boolean) => Promise<void>;
}): React.JSX.Element {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [domain, setDomain] = useState("local");
  const [guestName, setGuestName] = useState("");
  const [persistSession, setPersistSession] = useState(() => isAuthPersisted());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [productVersion, setProductVersion] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    client
      .getVersion()
      .then((info) => {
        if (!cancelled) {
          setProductVersion(displayProductVersion(info.version));
        }
      })
      .catch(() => {
        if (!cancelled) {
          setProductVersion("");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    event.stopPropagation();
    setError("");
    setSubmitting(true);
    try {
      if (props.mode === "setup") {
        if (password !== confirmPassword) {
          throw new Error("Passwords do not match.");
        }
        await props.onSetup(username, password, persistSession);
      } else {
        await props.onLogin(username, password, persistSession);
      }
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Authentication failed.");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleGuest(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await props.onGuest(guestName, persistSession);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Authentication failed.");
    } finally {
      setSubmitting(false);
    }
  }

  const year = new Date().getFullYear();
  const versionLabel = productVersion ? `Version ${productVersion}` : "Version unavailable";

  return (
    <div className="auth-layout">
      <section className="auth-visual">
        <div className="auth-visual__panel">
          <span className="eyebrow">Zon Appliance</span>
          <h1>Sleek infrastructure operations, built for simplicity.</h1>
          <p>
            A focused control-plane workspace for management, analysis, and administration without
            losing the appliance-grade clarity operators expect.
          </p>
          <div className="hero-diagram" aria-hidden="true">
            <div className="hero-diagram__frame">
              <div className="hero-diagram__sidebar" />
              <div className="hero-diagram__content">
                <div className="hero-diagram__header" />
                <div className="hero-diagram__cards">
                  <span />
                  <span />
                  <span />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
      <section className="auth-form">
        <header className="auth-form__logo">
          <BrandMark size="lg" />
        </header>
        <div className="auth-form__center">
          <div className="auth-form__intro">
            <p className="auth-form__welcome">Welcome to Zon Appliance</p>
            <p className="auth-form__version" aria-live="polite">
              {versionLabel}
            </p>
            {props.mode === "setup" ? (
              <p className="auth-form__lede">Create the first administrator to finish setup.</p>
            ) : null}
          </div>
            <form className="stack-form auth-form__fields" method="post" onSubmit={handleSubmit}>
              <label className="field">
                <span>Username</span>
                <input
                  name="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  autoComplete="username"
                  required
                />
              </label>
              <label className="field">
                <span>Password</span>
                <input
                  name="password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete={props.mode === "setup" ? "new-password" : "current-password"}
                  required
                />
              </label>
            {props.mode === "setup" ? (
              <label className="field">
                <span>Confirm password</span>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  autoComplete="new-password"
                />
              </label>
            ) : null}
            {props.mode === "setup" ? (
              <label className="field">
                <span>Domain</span>
                <select value={domain} onChange={(event) => setDomain(event.target.value)}>
                  <option value="local">local</option>
                </select>
              </label>
            ) : null}
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={persistSession}
                onChange={(event) => setPersistSession(event.target.checked)}
              />
              <span>Keep me signed in on this device</span>
            </label>
            {error ? <div className="message message--error">{error}</div> : null}
            <button className="button button--primary" disabled={submitting} type="submit">
              {submitting
                ? "Working..."
                : props.mode === "setup"
                  ? "Create administrator"
                  : "Sign in"}
            </button>
          </form>
          {props.mode === "login" && props.guestAccess ? (
            <form className="stack-form auth-form__guest" method="post" onSubmit={handleGuest}>
              <div className="auth-form__guest-heading">
                <span>Guest access</span>
                <p>Enter your name to continue as a guest.</p>
              </div>
              <label className="field">
                <span>Name</span>
                <input
                  name="guest-name"
                  value={guestName}
                  onChange={(event) => setGuestName(event.target.value)}
                  autoComplete="name"
                  required
                />
              </label>
              <button className="button button--secondary" disabled={submitting} type="submit">
                {submitting ? "Working..." : "Continue as guest"}
              </button>
            </form>
          ) : null}
        </div>
        <footer className="auth-form__footer">
          <p className="auth-form__copyright">© {year} Zon. All rights reserved.</p>
          <nav className="auth-form__legal" aria-label="Legal and support">
            <a href="#terms-and-conditions">Terms &amp; Conditions</a>
            <span aria-hidden="true">·</span>
            <a href="#help-center">Help Center</a>
          </nav>
        </footer>
      </section>
    </div>
  );
}
