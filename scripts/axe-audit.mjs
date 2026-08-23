import AxeBuilder from "@axe-core/playwright";
import { chromium } from "playwright-core";

const chromePath = process.env.CHROME_PATH;
const urls = process.argv.slice(2);

if (!chromePath) {
  throw new Error("CHROME_PATH is required");
}
if (urls.length === 0) {
  throw new Error("provide at least one URL to audit");
}

const browser = await chromium.launch({
  executablePath: chromePath,
  headless: true,
  args: ["--no-sandbox"],
});
const context = await browser.newContext({ colorScheme: "light" });

let failed = false;
try {
  for (const url of urls) {
    const page = await context.newPage();
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForTimeout(1_500);
    const result = await new AxeBuilder({ page }).analyze();
    if (result.violations.length === 0) {
      console.log(`${url}: axe pass`);
    } else {
      failed = true;
      console.error(`${url}: ${result.violations.length} axe violation(s)`);
      for (const violation of result.violations) {
        console.error(`  ${violation.id}: ${violation.help}`);
        for (const node of violation.nodes) {
          const summary = node.failureSummary?.replaceAll("\n", " ") ?? "";
          console.error(`    ${node.target.join(", ")}: ${summary}`);
        }
      }
    }
    await page.close();
  }
} finally {
  await context.close();
  await browser.close();
}

if (failed) process.exit(1);
