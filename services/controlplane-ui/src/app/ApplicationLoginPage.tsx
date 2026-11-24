import React, { useEffect, useState } from "react";
import { client } from "../lib/api";
import { displayProductVersion } from "../productVersion";
import { applicationAuth } from "../lib/applicationAuth";
import { navigate } from "../lib/navigate";

const PHONE_COUNTRIES = [
  { code: "IN", name: "India", dialCode: "+91", minDigits: 10, maxDigits: 10 },
  { code: "US", name: "United States", dialCode: "+1", minDigits: 10, maxDigits: 10 },
  { code: "GB", name: "United Kingdom", dialCode: "+44", minDigits: 10, maxDigits: 10 },
  { code: "AE", name: "United Arab Emirates", dialCode: "+971", minDigits: 9, maxDigits: 9 },
  { code: "SG", name: "Singapore", dialCode: "+65", minDigits: 8, maxDigits: 8 },
  { code: "AU", name: "Australia", dialCode: "+61", minDigits: 9, maxDigits: 9 }
] as const;

type PhoneCountry = (typeof PHONE_COUNTRIES)[number];

const DEFAULT_PHONE_COUNTRY = PHONE_COUNTRIES[0];

export function ApplicationLoginPage(props: { onAuthenticated: () => void }): React.JSX.Element {
  const [productVersion, setProductVersion] = useState("");
  const [phoneCountryCode, setPhoneCountryCode] = useState<string>(DEFAULT_PHONE_COUNTRY.code);
  const [phoneNumber, setPhoneNumber] = useState("");
  const [challengeID, setChallengeID] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const phoneCountry: PhoneCountry = PHONE_COUNTRIES.find(({ code: countryCode }) => countryCode === phoneCountryCode) ?? DEFAULT_PHONE_COUNTRY;

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
            <span>Powered by Zonited Solutions</span>
          </div>
        </div>
      </section>

      <section className="application-login__panel" aria-label="Zonited App sign in">
        <div className="application-login__form-wrap">
          <header className="application-login__brand" aria-label="Zonited App">
            <span className="application-login__brand-logo">
              <img src="/zonited-logo-square.png" alt="Zonited" />
            </span>
          </header>
          <p className="application-login__eyebrow">Mobile member access</p>
          <h2>{challengeID ? "Enter your verification code." : "Sign in"}</h2>
          {!challengeID ? (
            <form onSubmit={(event) => {
              event.preventDefault();
              const digits = phoneNumber.replace(/\D/g, "");
              if (digits.length < phoneCountry.minDigits || digits.length > phoneCountry.maxDigits) {
                setError(`Enter a ${phoneCountry.minDigits}-digit mobile number.`);
                return;
              }
              setError("");
              setBusy(true);
              void applicationAuth.challenge(`${phoneCountry.dialCode}${digits}`).then((result) => setChallengeID(result.challenge_id)).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not send a code.")).finally(() => setBusy(false));
            }}>
              <div className="application-login__phone-field">
                <label className="field-label" htmlFor="application-phone">Mobile number</label>
                <div className="application-login__phone-input">
                  <label className="application-login__country" htmlFor="application-country">
                    <select id="application-country" aria-label={`Country code (${phoneCountry.name})`} title={phoneCountry.name} value={phoneCountry.code} onChange={(event) => {
                      const nextCountry = PHONE_COUNTRIES.find(({ code: countryCode }) => countryCode === event.target.value) ?? DEFAULT_PHONE_COUNTRY;
                      setPhoneCountryCode(nextCountry.code);
                      setPhoneNumber((current) => current.replace(/\D/g, "").slice(0, nextCountry.maxDigits));
                      setError("");
                    }}>
                      {PHONE_COUNTRIES.map((country) => <option key={country.code} value={country.code}>{country.dialCode}</option>)}
                    </select>
                  </label>
                  <input id="application-phone" className="text-input" type="tel" inputMode="numeric" autoComplete="tel-national" placeholder={phoneCountry.code === "IN" ? "98765 43210" : "Mobile number"} value={phoneNumber} onChange={(event) => {
                    setPhoneNumber(event.target.value.replace(/\D/g, "").slice(0, phoneCountry.maxDigits));
                    setError("");
                  }} minLength={phoneCountry.minDigits} maxLength={phoneCountry.maxDigits} pattern={`[0-9]{${phoneCountry.minDigits},${phoneCountry.maxDigits}}`} required />
                </div>
              </div>
              {error ? <p className="form-error" role="alert">{error}</p> : null}
              <button className="application-login__submit" type="submit" disabled={busy}><span>{busy ? "Sending code…" : "Send verification code"}</span><svg aria-hidden="true" viewBox="0 0 20 20" focusable="false"><path d="M4 10h10M10 5l5 5-5 5" /></svg></button>
            </form>
          ) : (
            <form onSubmit={(event) => { event.preventDefault(); setError(""); setBusy(true); void applicationAuth.verify(challengeID, code.trim()).then(props.onAuthenticated).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not verify the code.")).finally(() => setBusy(false)); }}>
              <label className="field-label" htmlFor="application-code">Six-digit code</label>
              <input id="application-code" className="text-input" type="text" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))} required />
              {error ? <p className="form-error" role="alert">{error}</p> : null}
              <button className="application-login__submit" type="submit" disabled={busy}><span>{busy ? "Verifying…" : "Continue"}</span><svg aria-hidden="true" viewBox="0 0 20 20" focusable="false"><path d="M4 10h10M10 5l5 5-5 5" /></svg></button>
              <button className="application-login__secondary" type="button" onClick={() => { setChallengeID(""); setCode(""); setError(""); }}>Use a different number</button>
            </form>
          )}
        </div>
        <footer className="application-login__footer">
          <span>{productVersion ? `Zonited App ${productVersion}` : "Zonited App"}</span>
          <button type="button" onClick={() => navigate("/console/login", true)}>Operator console</button>
        </footer>
      </section>
    </main>
  );
}
