import React, { useEffect, useState } from "react";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { displayProductVersion } from "../productVersion";
import { applicationAuth } from "../lib/applicationAuth";

export function ApplicationLoginPage(props: { onAuthenticated: () => void }): React.JSX.Element {
  const [productVersion, setProductVersion] = useState("");
  const [phone, setPhone] = useState("");
  const [challengeID, setChallengeID] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

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

  return (
    <main className="application-login">
      <section className="application-login__story" aria-labelledby="application-login-title">
        <div className="application-login__story-inner">
          <div className="application-login__kicker">
            <span className="application-login__kicker-mark" aria-hidden="true" />
            Zonited App
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

      <section className="application-login__panel" aria-label="Zonited App sign in">
        <header className="application-login__brand">
          <span className="application-login__brand-logo" aria-hidden="true">
            <img src="/zonited-logo-circle.png" alt="" />
          </span>
          <span>Zonited</span>
        </header>
        <div className="application-login__form-wrap">
          <p className="application-login__eyebrow">Mobile member access</p>
          <h2>{challengeID ? "Enter your verification code." : "Sign in with your mobile."}</h2>
          <p className="application-login__lede">
            Zonited accounts use a verified mobile number. Appliance operator usernames and passwords are not accepted here.
          </p>
          {!challengeID ? (
            <form onSubmit={(event) => { event.preventDefault(); setError(""); setBusy(true); void applicationAuth.challenge(phone.trim()).then((result) => setChallengeID(result.challenge_id)).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not send a code.")).finally(() => setBusy(false)); }}>
              <label className="field-label" htmlFor="application-phone">Mobile number</label>
              <input id="application-phone" className="text-input" type="tel" inputMode="tel" autoComplete="tel" placeholder="+91 98765 43210" value={phone} onChange={(event) => setPhone(event.target.value)} required />
              {error ? <p className="form-error" role="alert">{error}</p> : null}
              <button className="application-login__submit" type="submit" disabled={busy}>{busy ? "Sending code…" : "Send verification code"}</button>
            </form>
          ) : (
            <form onSubmit={(event) => { event.preventDefault(); setError(""); setBusy(true); void applicationAuth.verify(challengeID, code.trim()).then(props.onAuthenticated).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not verify the code.")).finally(() => setBusy(false)); }}>
              <label className="field-label" htmlFor="application-code">Six-digit code</label>
              <input id="application-code" className="text-input" type="text" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))} required />
              {error ? <p className="form-error" role="alert">{error}</p> : null}
              <button className="application-login__submit" type="submit" disabled={busy}>{busy ? "Verifying…" : "Continue"}</button>
              <button className="button button--ghost" type="button" onClick={() => { setChallengeID(""); setCode(""); setError(""); }}>Use a different number</button>
            </form>
          )}
        </div>
        <footer className="application-login__footer">
          <span>{productVersion ? `Zonited App ${productVersion}` : "Zonited App"}</span>
          <button type="button" onClick={() => navigate("/console", true)}>Operations console</button>
        </footer>
      </section>
    </main>
  );
}
