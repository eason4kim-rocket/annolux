(() => {
  const api = "https://api.annolux.com/api/v1";
  const authKey = "annolux-auth";
  const storageKey = "annolux-theme";
  const root = document.documentElement;
  const button = document.querySelector("[data-theme-toggle]");
  const zh = root.lang.startsWith("zh");
  const defaultReturnPath = zh ? "/zh/account" : "/account";
  const allowedReturnPaths = zh
    ? ["/zh/account", "/zh/billing/checkout", "/zh/playground"]
    : ["/account", "/billing/checkout", "/playground"];
  const requestedReturnPath = new URLSearchParams(window.location.search).get("return");
  const returnPath = allowedReturnPaths.includes(requestedReturnPath)
    ? requestedReturnPath
    : defaultReturnPath;

  fetch(api + "/account/me", { credentials: "include" })
    .then((res) => {
      if (res.ok) {
        try { window.localStorage.setItem(authKey, "true"); } catch (_) {}
        window.location.assign(returnPath);
      } else {
        try { window.localStorage.removeItem(authKey); } catch (_) {}
      }
    })
    .catch(() => {});

  function applyTheme(theme, persist) {
    const next = theme === "dark" ? "dark" : "light";
    root.dataset.theme = next;
    const color = document.querySelector('meta[name="theme-color"]');
    if (color) color.content = next === "dark" ? (color.dataset.dark || "#0a111b") : (color.dataset.light || "#fcfcf9");
    button?.setAttribute("aria-pressed", String(next === "dark"));
    if (persist) {
      try { window.localStorage.setItem(storageKey, next); } catch (_) {}
    }
  }

  button?.addEventListener("click", () => applyTheme(root.dataset.theme === "dark" ? "light" : "dark", true));

  document.querySelectorAll('.auth-provider[href^="https://api.annolux.com/api/v1/auth/"]').forEach((provider) => {
    const target = new URL(provider.href);
    target.searchParams.set("return", returnPath);
    provider.href = target.toString();
  });
})();
