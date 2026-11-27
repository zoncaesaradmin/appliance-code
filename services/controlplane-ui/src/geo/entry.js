if (window.location.pathname === "/geo/login") {
  await import("./login.js");
} else {
  await import("./main.js");
}
