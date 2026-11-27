import React, { useEffect, useState } from "react";
import { client } from "../lib/api";
import { displayProductVersion } from "../productVersion";
import { applicationAuth, presentedAuthError } from "../lib/applicationAuth";
import {
  DEFAULT_PHONE_COUNTRY,
  PHONE_COUNTRIES,
  VERIFICATION_CODE_LENGTH,
  nationalDigits,
  type PhoneCountry
} from "../lib/phoneNumber";

export function ApplicationLoginPage(props: { onAuthenticated: () => void }): React.JSX.Element {
  const [productVersion, setProductVersion] = useState("");
  const [phoneCountryCode, setPhoneCountryCode] = useState<string>(DEFAULT_PHONE_COUNTRY.code);
  const [phoneNumber, setPhoneNumber] = useState("");
  const [challengeID, setChallengeID] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const year = new Date().getFullYear();
  const phoneCountry: PhoneCountry = PHONE_COUNTRIES.find(({ code: countryCode }) => countryCode === phoneCountryCode) ?? DEFAULT_PHONE_COUNTRY;
  const phoneDigits = nationalDigits(phoneNumber, phoneCountry.maxDigits);
  const codeDigits = nationalDigits(code, VERIFICATION_CODE_LENGTH);

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
      <div className="application-login__canvas" aria-hidden="true">
        <img src="/zonited-login-neighborhood.jpg" alt="" />
      </div>

      <section className="application-login__panel" aria-label="Zonited sign in">
        <div className="application-login__form-wrap">
          <header className="application-login__brand" aria-label="Zonited">
            <span className="application-login__brand-logo">
              <img src="/zonited-logo-square.png" alt="Zonited" />
            </span>
          </header>
          <h2>{challengeID ? "Enter your verification code." : "Sign in"}</h2>
          {!challengeID ? (
            <form onSubmit={(event) => {
              event.preventDefault();
              const digits = phoneDigits;
              if (digits.length < phoneCountry.minDigits || digits.length > phoneCountry.maxDigits) {
                setError(`Enter a ${phoneCountry.minDigits}-digit mobile number.`);
                return;
              }
              setError("");
              setBusy(true);
              void applicationAuth.challenge(`${phoneCountry.dialCode}${digits}`).then((result) => {
                if (!result.challenge_id) {
                  setError("Could not send a code. Try again.");
                  return;
                }
                setChallengeID(result.challenge_id);
              }).catch((reason: unknown) => setError(presentedAuthError(reason, "Could not send a code. Try again."))).finally(() => setBusy(false));
            }}>
              <div className="application-login__controls application-login__controls--phone" style={{ "--national-length": phoneCountry.maxDigits, "--dial-length": phoneCountry.dialCode.length } as React.CSSProperties}>
                <div className="application-login__phone-input">
                  <label className="application-login__country" htmlFor="application-country">
                    <select id="application-country" aria-label={`Country code (${phoneCountry.name})`} title={phoneCountry.name} value={phoneCountry.code} onChange={(event) => {
                      const nextCountry = PHONE_COUNTRIES.find(({ code: countryCode }) => countryCode === event.target.value) ?? DEFAULT_PHONE_COUNTRY;
                      setPhoneCountryCode(nextCountry.code);
                      setPhoneNumber((current) => nationalDigits(current, nextCountry.maxDigits));
                      setError("");
                    }}>
                      {PHONE_COUNTRIES.map((country) => <option key={country.code} value={country.code}>{country.flag} {country.dialCode}</option>)}
                    </select>
                  </label>
                  <input id="application-phone" className="text-input" type="tel" inputMode="numeric" autoComplete="tel-national" placeholder={phoneCountry.example} value={phoneDigits} onChange={(event) => {
                    setPhoneNumber(nationalDigits(event.target.value, phoneCountry.maxDigits));
                    setError("");
                  }} maxLength={phoneCountry.maxDigits} aria-label="Mobile number" required />
                </div>
                {error ? <p className="form-error" role="alert">{error}</p> : null}
                <button className="application-login__submit" type="submit" disabled={busy}><span>{busy ? "Sending code…" : "Send verification code"}</span><svg aria-hidden="true" viewBox="0 0 20 20" focusable="false"><path d="M4 10h10M10 5l5 5-5 5" /></svg></button>
              </div>
            </form>
          ) : (
            <form onSubmit={(event) => { event.preventDefault(); if (codeDigits.length !== VERIFICATION_CODE_LENGTH) { setError("Enter the six-digit code."); return; } setError(""); setBusy(true); void applicationAuth.verify(challengeID, codeDigits).then(props.onAuthenticated).catch((reason: unknown) => setError(presentedAuthError(reason, "Could not verify the code. Try again."))).finally(() => setBusy(false)); }}>
              <div className="application-login__controls application-login__controls--code" style={{ "--national-length": VERIFICATION_CODE_LENGTH } as React.CSSProperties}>
                <input id="application-code" className="text-input" type="text" inputMode="numeric" autoComplete="one-time-code" placeholder="123456" maxLength={VERIFICATION_CODE_LENGTH} value={codeDigits} aria-label="Six-digit code" onChange={(event) => { setCode(nationalDigits(event.target.value, VERIFICATION_CODE_LENGTH)); setError(""); }} required />
                {error ? <p className="form-error" role="alert">{error}</p> : null}
                <button className="application-login__submit" type="submit" disabled={busy}><span>{busy ? "Verifying…" : "Continue"}</span><svg aria-hidden="true" viewBox="0 0 20 20" focusable="false"><path d="M4 10h10M10 5l5 5-5 5" /></svg></button>
                <button className="application-login__secondary" type="button" onClick={() => { setChallengeID(""); setCode(""); setError(""); }}>Use a different number</button>
              </div>
            </form>
          )}
        </div>
        <footer className="application-login__footer">
          <span>{productVersion ? `Zonited ${productVersion}` : "Zonited"}</span>
          <span>© {year} Zonited. All rights reserved.</span>
        </footer>
      </section>
    </main>
  );
}
