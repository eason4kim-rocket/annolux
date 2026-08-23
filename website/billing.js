(() => {
  const api = "https://api.annolux.com/api/v1";
  const zh = document.documentElement.lang.startsWith("zh");
  const status = document.querySelector("[data-billing-status]");
  const form = document.querySelector("[data-billing-form]");
  const planStorageKey = "annolux-checkout-plan";
  const checkoutReturnPath = zh ? "/zh/billing/checkout" : "/billing/checkout";
  const requestedPlan = strictPlan(new URLSearchParams(window.location.search).get("plan"));
  const storedPlan = readStoredPlan();
  const initialPlan = requestedPlan || storedPlan || "pro";
  let csrf = "";
  let initializedToken = "";

  function strictPlan(value) {
    return value === "pro" || value === "scale" ? value : "";
  }

  function readStoredPlan() {
    try {
      return strictPlan(window.sessionStorage.getItem(planStorageKey));
    } catch (_) {
      return "";
    }
  }

  function rememberPlan(value) {
    const plan = strictPlan(value);
    if (!plan) return;
    try { window.sessionStorage.setItem(planStorageKey, plan); } catch (_) {}
  }

  function redirectToLogin() {
    rememberPlan(form?.elements.plan?.value || initialPlan);
    const login = new URL(zh ? "/zh/login" : "/login", window.location.origin);
    login.searchParams.set("return", checkoutReturnPath);
    window.location.assign(login.pathname + login.search);
  }

  async function apiRequest(path, options = {}) {
    const response = await fetch(api + path, {
      credentials: "include",
      cache: "no-store",
      ...options,
      headers: {
        ...(options.body ? { "Content-Type": "application/json" } : {}),
        ...(csrf ? { "X-CSRF-Token": csrf } : {}),
        ...(options.headers || {})
      }
    });
    if (response.status === 401) {
      redirectToLogin();
      throw new Error("login required");
    }
    const body = response.status === 204 ? null : await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body?.error?.message || `HTTP ${response.status}`);
    return body;
  }

  function loadPaddle() {
    if (window.Paddle) return Promise.resolve(window.Paddle);
    return new Promise((resolve, reject) => {
      const script = document.createElement("script");
      script.src = "https://cdn.paddle.com/paddle/v2/paddle.js";
      script.async = true;
      script.addEventListener("load", () => resolve(window.Paddle));
      script.addEventListener("error", () => reject(new Error(zh ? "无法加载 Paddle Checkout。" : "Paddle Checkout could not be loaded.")));
      document.head.append(script);
    });
  }

  async function openCheckout(intent) {
    if (!intent || !["sandbox", "live"].includes(intent.environment) || !String(intent.price_id).startsWith("pri_") || !String(intent.checkout_intent).startsWith("chk_")) {
      throw new Error(zh ? "服务端返回了无效的结账信息。" : "The server returned invalid checkout data.");
    }
    if (intent.environment === "sandbox" && !String(intent.client_token).startsWith("test_")) {
      throw new Error(zh ? "Sandbox client token 无效。" : "The Sandbox client token is invalid.");
    }
    if (intent.environment === "live" && !String(intent.client_token).startsWith("live_")) {
      throw new Error(zh ? "Live client token 无效。" : "The Live client token is invalid.");
    }
    const paddle = await loadPaddle();
    if (!paddle) throw new Error(zh ? "Paddle Checkout 不可用。" : "Paddle Checkout is unavailable.");
    if (!initializedToken) {
      if (intent.environment === "sandbox") paddle.Environment.set("sandbox");
      paddle.Initialize({ token: intent.client_token });
      initializedToken = intent.client_token;
    } else if (initializedToken !== intent.client_token) {
      throw new Error(zh ? "结账配置已变化，请刷新页面。" : "Checkout configuration changed; reload this page.");
    }
    paddle.Checkout.open({
      items: [{ priceId: intent.price_id, quantity: 1 }],
      customData: intent.custom_data,
      settings: {
        displayMode: "overlay",
        variant: "one-page",
        theme: document.documentElement.dataset.theme === "dark" ? "dark" : "light",
        locale: zh ? "zh-Hans" : "en",
        successUrl: zh ? "https://annolux.com/zh/checkout/success" : "https://annolux.com/checkout/success"
      }
    });
  }

  form?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const fields = new FormData(form);
    const button = form.querySelector("button[type=submit]");
    rememberPlan(String(fields.get("plan") || ""));
    button.disabled = true;
    status.textContent = zh ? "正在创建一次性结账意图…" : "Creating a one-time checkout intent…";
    try {
      const intent = await apiRequest("/billing/checkout-intents", {
        method: "POST",
        body: JSON.stringify({
          plan: String(fields.get("plan") || ""),
          accept_terms: fields.get("accept_terms") === "on",
          accept_refund: fields.get("accept_refund") === "on"
        })
      });
      await openCheckout(intent);
      status.textContent = zh ? "结账已打开。点数只会在服务端验证交易后发放。" : "Checkout opened. Credits are granted only after server-side transaction verification.";
    } catch (error) {
      status.textContent = error.message || String(error);
    } finally {
      button.disabled = false;
    }
  });

  if (initialPlan) {
    const selected = form?.querySelector(`input[name="plan"][value="${initialPlan}"]`);
    if (selected) selected.checked = true;
  }

  form?.addEventListener("change", (event) => {
    if (event.target instanceof HTMLInputElement && event.target.name === "plan") {
      rememberPlan(event.target.value);
    }
  });

  rememberPlan(initialPlan);

  apiRequest("/account/me").then((me) => {
    csrf = me.csrf_token;
    form.hidden = false;
    status.textContent = zh ? "选择套餐并接受条款后，将打开 Paddle 安全结账。" : "Choose a plan and accept the policies to open secure Paddle checkout.";
  }).catch((error) => {
    if (error.message !== "login required") status.textContent = error.message || String(error);
  });
})();
