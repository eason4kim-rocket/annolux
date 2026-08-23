(() => {
  const api = "https://api.annolux.com/api/v1";
  const zh = document.documentElement.lang.startsWith("zh");
  const message = document.querySelector("[data-checkout-message]");
  const balance = document.querySelector("[data-checkout-balance]");
  let attempts = 0;

  async function poll() {
    attempts += 1;
    try {
      const response = await fetch(api + "/account/usage", { credentials: "include", cache: "no-store" });
      if (response.status === 401) { window.location.assign(zh ? "/zh/login" : "/login"); return; }
      if (!response.ok) throw new Error();
      const usage = await response.json();
      balance.textContent = (zh ? "当前可用点数：" : "Current available credits: ") + Number(usage.available).toLocaleString();
      message.textContent = zh ? "服务端正在核对已签名交易事件；本页不会自行发放点数。" : "The server is reconciling the signed transaction event; this page cannot grant credits.";
    } catch (_) {
      message.textContent = zh ? "暂时无法读取账户，请稍后重试。" : "The account service is temporarily unavailable.";
    }
    if (attempts < 60) window.setTimeout(poll, 2000);
  }
  poll();
})();
