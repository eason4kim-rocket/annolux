(() => {
  const api = "https://api.annolux.com/api/v1";
  const zh = document.documentElement.lang.startsWith("zh");
  const form = document.querySelector("[data-playground-form]");
  const status = document.querySelector("[data-playground-status]");
  const output = document.querySelector("[data-playground-output]");
  let csrf = "";
  let demoMode = false;

  const demoNotice = zh
    ? "体验模式——免注册：最多 3 条结果、每分钟 5 次。登录解锁完整额度。"
    : "Demo mode — no signup needed: up to 3 results, 5 searches/min. Sign in for full limits.";

  const session = fetch(api + "/account/me", { credentials: "include" }).then(async (response) => {
    if (response.status === 401) { demoMode = true; status.textContent = demoNotice; return; }
    const data = await response.json(); csrf = data.csrf_token;
  }).catch(() => { demoMode = true; status.textContent = demoNotice; });

  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    status.textContent = zh ? "正在准备…" : "Preparing…";
    await session;
    if (!csrf && !demoMode) {
      demoMode = true;
      status.textContent = demoNotice;
    }
    const data = new FormData(form);
    const domains = String(data.get("domains") || "").split(",").map((value) => value.trim()).filter(Boolean);
    const body = { query: String(data.get("query") || ""), deduplicate: data.get("deduplicate") === "on", limit: Number(data.get("limit")), timeout: Number(data.get("timeout")) };
    if (domains.length) body.domains = domains;
    status.textContent = zh ? "搜索中…" : "Searching…";
    output.parentElement.hidden = true;
    try {
      const response = demoMode
        ? await fetch(api + "/demo/search", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
        : await fetch(api + "/playground/search", { method: "POST", credentials: "include", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(body) });
      const result = await response.json();
      if (response.status === 429 && demoMode) throw new Error(zh ? "体验额度已满（每分钟 5 次）——登录解锁完整额度。" : "Demo limit reached (5/min) — sign in for full limits.");
      if (!response.ok) throw new Error(result?.error?.message || `HTTP ${response.status}`);
      output.textContent = JSON.stringify(result, null, 2);
      output.parentElement.hidden = false;
      status.textContent = demoMode
        ? (zh ? "体验搜索——不扣点。" : "Demo search — 0 credits used.")
        : (zh ? "剩余点数：" : "Credits remaining: ") + (response.headers.get("X-Annolux-Credits-Remaining") || "—");
    } catch (error) { status.textContent = error.message; }
  });
})();
