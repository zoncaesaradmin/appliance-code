import React, { useEffect, useState } from "react";
import { client } from "../lib/api";
import { navigate } from "../lib/navigate";
import { displayProductVersion } from "../productVersion";

export function ApplicationLoginPage(): React.JSX.Element {
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
          <h2>Mobile sign-in is coming.</h2>
          <p className="application-login__lede">
            Zonited accounts will sign in with a verified mobile number. Appliance operator usernames and passwords
            are not accepted here.
          </p>
          <div className="application-login__notice" role="status">
            Mobile authentication is not enabled on this appliance yet. No operator credentials are sent from this
            page.
          </div>
          <button className="application-login__submit" type="button" disabled>
            Mobile sign-in unavailable
          </button>
        </div>
        <footer className="application-login__footer">
          <span>{productVersion ? `Zonited App ${productVersion}` : "Zonited App"}</span>
          <button type="button" onClick={() => navigate("/console", true)}>Operations console</button>
        </footer>
      </section>
    </main>
  );
}
