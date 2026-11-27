import { clearAuth, isAuthPersisted, saveAuth, setAuthPersist } from "../auth.ts";
import { client } from "../lib/api.ts";
import "./login.css";

const root = document.querySelector("#geo-app");
const params = new URLSearchParams(window.location.search);
const requestedNext = params.get("next") || "";
const next = requestedNext.startsWith("/geo") && !requestedNext.startsWith("/geo/login") ? requestedNext : "/geo";

root.innerHTML = `
  <main class="geo-login">
    <section class="geo-login__visual" aria-hidden="true">
      <div class="geo-login__mark">◈</div>
      <p class="geo-login__eyebrow">ZONITED · GEO SERVICES</p>
      <h1>Define a place.</h1>
      <p>Map the boundaries that connect a place to its community.</p>
      <div class="geo-login__grid"></div>
    </section>
    <section class="geo-login__panel">
      <div class="geo-login__brand">GEO-ZONE <span>GEOGRAPHIC WORKSPACE</span></div>
      <p class="geo-login__kicker">Employee sign in</p>
      <h2>Welcome back.</h2>
      <p class="geo-login__lede">Use your appliance account to open the Geo-Zone workspace.</p>
      <form id="geo-login-form" class="geo-login__form">
        <label>Username<input name="username" autocomplete="username" required autofocus></label>
        <label>Password<input name="password" type="password" autocomplete="current-password" required></label>
        <label class="geo-login__persist"><input name="persist" type="checkbox"> Keep me signed in on this device</label>
        <p id="geo-login-error" class="geo-login__error" role="alert"></p>
        <button type="submit">Open Geo-Zone</button>
      </form>
      <a class="geo-login__console" href="/console/login">Appliance operator? Open the console login.</a>
    </section>
  </main>`;

try {
  const setup = await client.getSetupStatus();
  if (!setup.initialized) {
    window.location.replace("/setup");
  } else {
    await client.getSession();
    window.location.replace(next);
  }
} catch {
  clearAuth();
}

const form = document.querySelector("#geo-login-form");
const error = document.querySelector("#geo-login-error");
const persist = form.elements.namedItem("persist");
persist.checked = isAuthPersisted();
form.addEventListener("submit", async (event) => {
  event.preventDefault();
  error.textContent = "";
  const submit = form.querySelector("button");
  submit.disabled = true;
  try {
    const username = form.elements.namedItem("username").value.trim();
    const password = form.elements.namedItem("password").value;
    setAuthPersist(persist.checked);
    saveAuth(await client.login(username, password, "local"));
    window.location.replace(next);
  } catch (loginError) {
    clearAuth();
    error.textContent = loginError instanceof Error ? loginError.message : "Authentication failed.";
    submit.disabled = false;
  }
});
