import { chromium } from "playwright-core";

const chromePath = process.env.CHROME_PATH;
const baseURL = (process.argv[2] || "http://127.0.0.1:8090").replace(/\/$/, "");

if (!chromePath) {
  throw new Error("CHROME_PATH is required");
}

const routes = [
  { path: "/", darkColor: "#000000", login: false },
  { path: "/zh/", darkColor: "#000000", login: false },
  { path: "/docs", darkColor: "#070b0e", login: false },
  { path: "/zh/docs", darkColor: "#070b0e", login: false },
  { path: "/login", darkColor: "#0a111b", login: true },
  { path: "/zh/login", darkColor: "#0a111b", login: true },
];

function check(condition, message) {
  if (!condition) throw new Error(message);
}

const browser = await chromium.launch({
  executablePath: chromePath,
  headless: true,
  args: ["--no-sandbox"],
});
const context = await browser.newContext({ colorScheme: "light" });
await context.addInitScript(() => window.localStorage.setItem("annolux-theme", "dark"));

try {
  const bootResponse = await context.request.get(`${baseURL}/theme-init.js`);
  check(bootResponse.status() === 200, `theme-init.js returned ${bootResponse.status()}`);
  check((bootResponse.headers()["content-type"] || "").includes("javascript"), "theme-init.js has the wrong content type");

  for (const route of routes) {
    const cspErrors = [];
    const page = await context.newPage();
    page.on("console", (message) => {
      if (/content security policy|refused to (?:execute|apply|load).*?(?:script|style)/i.test(message.text())) {
        cspErrors.push(message.text());
      }
    });
    page.on("pageerror", (error) => cspErrors.push(`page error: ${error.message}`));

    const response = await page.goto(`${baseURL}${route.path}`, { waitUntil: "networkidle" });
    check(response?.status() === 200, `${route.path} returned ${response?.status()}`);
    const state = await page.evaluate(() => {
      const boot = document.querySelector('script[src="/theme-init.js"]');
      const firstStylesheet = document.querySelector('link[rel="stylesheet"]');
      const legal = document.querySelector(".auth-legal");
      const legalStyle = legal ? getComputedStyle(legal) : null;
      return {
        theme: document.documentElement.dataset.theme,
        themeColor: document.querySelector('meta[name="theme-color"]')?.content,
        inlineExecutableScripts: [...document.querySelectorAll("script:not([src])")]
          .filter((script) => script.type !== "application/ld+json").length,
        bootIsSynchronous: Boolean(boot && !boot.defer && !boot.async),
        bootPrecedesCSS: Boolean(boot && firstStylesheet && (boot.compareDocumentPosition(firstStylesheet) & Node.DOCUMENT_POSITION_FOLLOWING)),
        legal: legalStyle ? {
          display: legalStyle.display,
          gap: Number.parseFloat(legalStyle.columnGap),
          links: legal.querySelectorAll("a").length,
        } : null,
      };
    });

    check(state.theme === "dark", `${route.path} did not synchronously restore the saved dark theme`);
    check(state.themeColor === route.darkColor, `${route.path} theme-color is ${state.themeColor}, expected ${route.darkColor}`);
    check(state.inlineExecutableScripts === 0, `${route.path} contains an inline executable script`);
    check(state.bootIsSynchronous, `${route.path} theme boot is not a synchronous external script`);
    check(state.bootPrecedesCSS, `${route.path} theme boot does not precede the first stylesheet`);
    if (route.login) {
      check(state.legal?.display === "flex", `${route.path} auth legal navigation is not flex`);
      check(state.legal?.links === 4, `${route.path} auth legal navigation does not contain four links`);
      check(state.legal?.gap > 0, `${route.path} auth legal links have no visual gap`);
    }
    check(cspErrors.length === 0, `${route.path} CSP/browser errors:\n${cspErrors.join("\n")}`);
    console.log(`${route.path}: browser regression pass`);
    await page.close();
  }
} finally {
  await context.close();
  await browser.close();
}
