(() => {
  const api = "https://api.annolux.com/api/v1";
  const authKey = "annolux-auth";
  const zh = document.documentElement.lang.startsWith("zh");
  const status = document.querySelector("[data-account-status]");
  let csrf = "";

  async function request(path, options = {}) {
    const response = await fetch(api + path, {
      credentials: "include", ...options,
      headers: { ...(options.body ? { "Content-Type": "application/json" } : {}), ...(csrf ? { "X-CSRF-Token": csrf } : {}), ...(options.headers || {}) }
    });
    if (response.status === 401) {
      try { window.localStorage.removeItem(authKey); } catch (_) {}
      window.location.assign((zh ? "/zh/login" : "/login") + "?return=" + encodeURIComponent(zh ? "/zh/account" : "/account"));
      throw new Error("login required");
    }
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      const error = new Error(body?.error?.message || body?.message || `HTTP ${response.status}`);
      error.code = body?.error?.code || "";
      error.status = response.status;
      throw error;
    }
    if (response.status === 204) return null;
    return response.json();
  }

  function showError(error) {
    status.textContent = error.message || String(error);
    status.dataset.state = "error";
    status.scrollIntoView({ block: "nearest" });
  }

  function renderKeys(keys) {
    const target = document.querySelector("[data-key-list]");
    target.replaceChildren(...keys.map((key) => {
      const row = document.createElement("div");
      row.className = "account-row";
      const label = document.createElement("span");
      label.textContent = `${key.name} · ${key.prefix}`;
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = zh ? "撤销" : "Revoke";
      button.addEventListener("click", async () => {
        if (!window.confirm(zh ? "确定撤销此密钥？" : "Revoke this key?")) return;
        try { await request(`/account/api-keys/${encodeURIComponent(key.id)}`, { method: "DELETE" }); await loadKeys(); }
        catch (error) { showError(error); }
      });
      row.append(label, button);
      return row;
    }));
    if (!keys.length) target.textContent = zh ? "暂无 API Key。" : "No API keys yet.";
  }

  async function loadKeys() {
    const data = await request("/account/api-keys");
    renderKeys(data.api_keys || []);
  }

  async function load() {
    try {
      const me = await request("/account/me");
      csrf = me.csrf_token;
      try { window.localStorage.setItem(authKey, "true"); } catch (_) {}
      const [usage, identities] = await Promise.all([request("/account/usage"), request("/account/identities"), loadKeys()]);
      document.querySelector("[data-account-id]").textContent = me.id;
      document.querySelector("[data-account-state]").textContent = me.status;
      document.querySelector("[data-credit-balance]").textContent = Number(usage.available).toLocaleString();
      document.querySelector("[data-credit-detail]").textContent = zh ? `已用 ${usage.used} · 预占 ${usage.reserved}` : `Used ${usage.used} · reserved ${usage.reserved}`;
      const list = document.querySelector("[data-identity-list]");
      list.replaceChildren(...(identities.identities || []).map((identity) => {
        const row = document.createElement("div"); row.className = "account-row";
        row.textContent = `${identity.provider} · ${identity.display_name || identity.email || identity.subject}`;
        return row;
      }));
      document.querySelectorAll("[data-account-content]").forEach((node) => { node.hidden = false; });
      status.textContent = "";
      delete status.dataset.state;
    } catch (error) {
      if (error.message !== "login required") showError(error);
    }
  }

  document.querySelector("[data-key-form]")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    try {
      const created = await request("/account/api-keys", { method: "POST", body: JSON.stringify({ name: String(form.get("name") || "") }) });
      document.querySelector("[data-secret-value]").textContent = created.secret;
      document.querySelector("[data-secret-once]").hidden = false;
      event.currentTarget.reset();
      await loadKeys();
    } catch (error) { showError(error); }
  });
  async function linkIdentity(provider) {
    try {
      const returnPath = encodeURIComponent(zh ? "/zh/account" : "/account");
      const started = await request(`/account/identities/${provider}/start?return=${returnPath}`, { method: "POST" });
      const destination = new URL(started.authorization_url);
      const expectedHost = provider === "google" ? "accounts.google.com" : "github.com";
      if (destination.protocol !== "https:" || destination.hostname !== expectedHost || destination.username || destination.password) {
        throw new Error(zh ? "身份绑定地址无效。" : "The identity-link URL is invalid.");
      }
      window.location.assign(destination.href);
    } catch (error) { showError(error); }
  }
  document.querySelector("[data-link-google]")?.addEventListener("click", () => linkIdentity("google"));
  document.querySelector("[data-link-github]")?.addEventListener("click", () => linkIdentity("github"));
  document.querySelector("[data-copy-secret]")?.addEventListener("click", async () => {
    await navigator.clipboard.writeText(document.querySelector("[data-secret-value]").textContent);
  });
  document.querySelector("[data-billing-portal]")?.addEventListener("click", async () => {
    try {
      const session = await request("/billing/portal-sessions", { method: "POST" });
      const destination = new URL(session.url);
      if (destination.protocol !== "https:" || destination.username || destination.password || !(destination.hostname === "paddle.com" || destination.hostname.endsWith(".paddle.com"))) {
        throw new Error(zh ? "账单门户地址无效。" : "The billing portal URL is invalid.");
      }
      window.location.assign(destination.href);
    } catch (error) {
      if (error.code === "NO_SUBSCRIPTION" || error.status === 404) {
        window.location.assign(zh ? "/zh/pricing" : "/pricing");
      } else { showError(error); }
    }
  });
  document.querySelector("[data-logout]")?.addEventListener("click", async () => {
    if (!window.confirm(zh ? "确定退出登录？" : "Sign out of Annolux?")) return;
    try {
      await request("/auth/logout", { method: "POST" });
    } finally {
      try { window.localStorage.removeItem(authKey); } catch (_) {}
      window.location.assign(zh ? "/zh/" : "/");
    }
  });
  load();
})();
