import React, { FormEvent, useEffect, useState } from "react";
import { BrandMark } from "../components";
import { isAuthPersisted } from "../auth";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { displayProductVersion } from "../productVersion";

export function ApplicationLoginPage(props: {
  onLogin: (username: string, password: string, persist: boolean) => Promise<void>;
  guestAccess: boolean;
  onGuest: (name: string, persist: boolean) => Promise<void>;
}): React.JSX.Element {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [guestName, setGuestName] = useState("");
  const [persistSession, setPersistSession] = useState(() => isAuthPersisted());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [productVersion, setProductVersion] = useState("");

  useEffect(() => {
    let cancelled = false;
    void client
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

  async function submitLogin(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await props.onLogin(username.trim(), password, persistSession);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Could not sign in.");
    } finally {
      setSubmitting(false);
    }
  }

  async function submitGuest(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await props.onGuest(guestName.trim(), persistSession);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Could not continue as a guest.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="application-login">
      <section className="application-login__story" aria-labelledby="application-login-title">
        <div className="application-login__story-inner">
          <div className="application-login__kicker">
            <span className="application-login__kicker-mark" aria-hidden="true" />
            Your Zonity
          </div>
          <h1 id="application-login-title">A place that works around your microzone.</h1>
          <p>
            Sign in to discover local services, manage your orders, and stay connected to the people and places
            around you.
          </p>
          <div className="application-login__signal" aria-hidden="true">
            <span className="application-login__signal-line" />
            <span className="application-login__signal-node application-login__signal-node--one" />
            <span className="application-login__signal-node application-login__signal-node--two" />
            <span className="application-login__signal-node application-login__signal-node--three" />
          </div>
          <div className="application-login__note">
            <span>Private by design</span>
            <span>Delivered by Zon</span>
          </div>
        </div>
      </section>

      <section className="application-login__panel" aria-label="Zonity sign in">
        <header className="application-login__brand">
          <BrandMark size="lg" />
          <span>Zonity</span>
        </header>
        <div className="application-login__form-wrap">
          <p className="application-login__eyebrow">Member access</p>
          <h2>Welcome back.</h2>
          <p className="application-login__lede">Use your Zon account to continue to your local experience.</p>
          <form className="application-login__form" onSubmit={submitLogin}>
            <label className="application-login__field">
              <span>Username</span>
              <input
                name="username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                autoComplete="username"
                autoFocus
                required
              />
            </label>
            <label className="application-login__field">
              <span>Password</span>
              <input
                name="password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="current-password"
                required
              />
            </label>
            <label className="application-login__remember">
              <input
                type="checkbox"
                checked={persistSession}
                onChange={(event) => setPersistSession(event.target.checked)}
              />
              <span>Keep me signed in on this device</span>
            </label>
            {error ? <p className="application-login__error" role="alert">{error}</p> : null}
            <button className="application-login__submit" type="submit" disabled={submitting}>
              {submitting ? "Signing in…" : "Continue to Zonity"}
            </button>
          </form>
          {props.guestAccess ? (
            <form className="application-login__guest" onSubmit={submitGuest}>
              <label className="application-login__field">
                <span>Guest name</span>
                <input
                  name="guest-name"
                  value={guestName}
                  onChange={(event) => setGuestName(event.target.value)}
                  autoComplete="name"
                  required
                />
              </label>
              <button type="submit" disabled={submitting}>Continue as guest</button>
            </form>
          ) : null}
        </div>
        <footer className="application-login__footer">
          <span>{productVersion ? `Zonity ${productVersion}` : "Zonity application"}</span>
          <button type="button" onClick={() => navigate("/console", true)}>Operations console</button>
        </footer>
      </section>
    </main>
  );
}
