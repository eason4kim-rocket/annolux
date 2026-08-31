#!/usr/bin/env node

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import process from "node:process";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const packageJSON = JSON.parse(
  await readFile(path.join(root, "packages/annolux-mcp/package.json"), "utf8"),
);
const executable = process.argv[2] || path.join(root, "packages/annolux-mcp/bin/annolux-mcp.js");

const api = http.createServer((request, response) => {
  let body = "";
  request.setEncoding("utf8");
  request.on("data", (chunk) => {
    body += chunk;
  });
  request.on("end", () => {
    assert.equal(request.method, "POST");
    assert.equal(request.url, "/api/v1/demo/search");
    assert.equal(request.headers.authorization, undefined);
    assert.deepEqual(JSON.parse(body), {
      query: "MCP installation smoke test",
      limit: 1,
    });
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({
      results: [{
        title: "Annolux MCP smoke result",
        url: "https://example.com/mcp",
        snippet: "sandbox request reached the Search endpoint",
        fetched_at: "2026-08-31T00:00:00Z",
      }],
    }));
  });
});

await new Promise((resolve, reject) => {
  api.once("error", reject);
  api.listen(0, "127.0.0.1", resolve);
});

const address = api.address();
assert(address && typeof address !== "string");
const child = spawn(executable, [], {
  env: {
    ...process.env,
    ANNOLUX_API_URL: `http://127.0.0.1:${address.port}`,
    ANNOLUX_API_KEY: "",
  },
  stdio: ["pipe", "pipe", "pipe"],
});

let stderr = "";
child.stderr.setEncoding("utf8");
child.stderr.on("data", (chunk) => {
  stderr += chunk;
});

const pending = new Map();
const lines = readline.createInterface({ input: child.stdout });
lines.on("line", (line) => {
  const message = JSON.parse(line);
  const waiter = pending.get(message.id);
  if (waiter) {
    pending.delete(message.id);
    waiter.resolve(message);
  }
});

let nextID = 1;
function request(method, params) {
  const id = nextID++;
  const message = { jsonrpc: "2.0", id, method };
  if (params !== undefined) message.params = params;
  child.stdin.write(`${JSON.stringify(message)}\n`);
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`timed out waiting for ${method}`));
    }, 5000);
    pending.set(id, {
      resolve(value) {
        clearTimeout(timer);
        resolve(value);
      },
    });
  });
}

try {
  const initialized = await request("initialize", {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "annolux-package-smoke", version: "1.0.0" },
  });
  assert.equal(initialized.result.serverInfo.name, "annolux");
  assert.equal(initialized.result.serverInfo.version, packageJSON.version);

  child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" })}\n`);
  const listed = await request("tools/list");
  assert.deepEqual(listed.result.tools.map((tool) => tool.name), ["search_web"]);

  const searched = await request("tools/call", {
    name: "search_web",
    arguments: { query: "MCP installation smoke test", limit: 1 },
  });
  assert.equal(searched.result.isError, undefined);
  const payload = JSON.parse(searched.result.content[0].text);
  assert.equal(payload.results.length, 1);
  assert.match(stderr, /free sandbox mode/);
  process.stdout.write(`annolux-mcp ${packageJSON.version}: package handshake and sandbox search pass\n`);
} finally {
  child.kill("SIGTERM");
  lines.close();
  await new Promise((resolve) => api.close(resolve));
}
