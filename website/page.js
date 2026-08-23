(() => {
  const root = document.documentElement;
  const button = document.querySelector("[data-theme-toggle]");
  const key = "annolux-theme";
  const authKey = "annolux-auth";
  const api = "https://api.annolux.com/api/v1";
  const zh = root.lang.startsWith("zh");

  let stored = null;
  try { stored = window.localStorage.getItem(key); } catch (_) {}
  const initial = stored === "dark" || stored === "light" ? stored : (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const apply = (theme, persist = false) => {
    root.dataset.theme = theme;
    const themeColor = document.querySelector('meta[name="theme-color"]');
    if (themeColor) themeColor.content = theme === "dark" ? (themeColor.dataset.dark || "#070b0e") : (themeColor.dataset.light || "#fcfcf9");
    button?.setAttribute("aria-pressed", String(theme === "dark"));
    if (persist) { try { window.localStorage.setItem(key, theme); } catch (_) {} }
  };
  apply(initial);
  button?.addEventListener("click", () => apply(root.dataset.theme === "dark" ? "light" : "dark", true));

  function updateNavAuth(isAuth) {
    const signinLinks = document.querySelectorAll(".nav-signin, .mobile-nav a[href*='login']");
    signinLinks.forEach((link) => {
      if (isAuth) {
        link.href = zh ? "/zh/account" : "/account";
        link.textContent = zh ? "账户" : "Account";
      } else {
        link.href = zh ? "/zh/login" : "/login";
        link.textContent = zh ? "登录" : "Sign in";
      }
    });
  }

  let cachedAuth = false;
  try { cachedAuth = window.localStorage.getItem(authKey) === "true"; } catch (_) {}
  if (cachedAuth) updateNavAuth(true);

  fetch(api + "/account/me", { credentials: "include" })
    .then((res) => {
      const isAuth = res.ok;
      try {
        if (isAuth) window.localStorage.setItem(authKey, "true");
        else window.localStorage.removeItem(authKey);
      } catch (_) {}
      updateNavAuth(isAuth);
    })
    .catch(() => {});

  const liveStatus = document.querySelector("[data-live-status]");
  if (liveStatus) {
    const label = liveStatus.querySelector("strong");
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 5000);
    fetch("https://api.annolux.com/api/v1/ready", { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        const body = await response.json().catch(() => ({}));
        if (!response.ok || body.status !== "ready") throw new Error("not ready");
        liveStatus.dataset.state = "ready";
        label.textContent = zh ? "API、账户库与搜索索引均已就绪。" : "API, account store, and search index are ready.";
      })
      .catch(() => {
        liveStatus.dataset.state = "down";
        label.textContent = zh ? "公网 readiness 当前不可用；正在调查。" : "Public readiness is unavailable; investigation is required.";
      })
      .finally(() => window.clearTimeout(timeout));
  }
})();
