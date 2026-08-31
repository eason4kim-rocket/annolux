#!/usr/bin/env node
const readline = require("readline");

const VERSION = "0.2.1";
const PROTOCOL_VERSION = "2024-11-05";

function parseArgs() {
  const args = process.argv.slice(2);
  let apiKey = process.env.ANNOLUX_API_KEY || "";
  let apiUrl = process.env.ANNOLUX_API_URL || "https://api.annolux.com";

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "-key" || arg === "--key" || arg === "-api-key" || arg === "--api-key") {
      apiKey = args[++i] || "";
    } else if (arg.startsWith("-key=") || arg.startsWith("--key=")) {
      apiKey = arg.split("=")[1];
    } else if (arg === "-url" || arg === "--url") {
      apiUrl = args[++i] || "";
    } else if (arg.startsWith("-url=") || arg.startsWith("--url=")) {
      apiUrl = arg.split("=")[1];
    }
  }

  return { apiKey: apiKey.trim(), apiUrl: apiUrl.trim().replace(/\/+$/, "") };
}

const { apiKey, apiUrl } = parseArgs();

if (!apiKey) {
  process.stderr.write("ANNOLUX_API_KEY is required. Pass via -key <key> or ANNOLUX_API_KEY env var.\n");
  process.exit(1);
}

const TOOL_DEFINITION = {
  name: "search_web",
  description: "Search Annolux curated English and Chinese index. One successful call uses one credit.",
  inputSchema: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Natural-language query containing one to fifty words",
        maxLength: 500
      },
      limit: {
        type: "integer",
        description: "Maximum results (default 10, maximum 10)",
        minimum: 1,
        maximum: 10,
        default: 10
      },
      domains: {
        type: "array",
        description: "Optional registrable-domain filters without schemes or paths",
        items: {
          type: "string"
        }
      },
      deduplicate: {
        type: "boolean",
        description: "Collapse duplicate and near-duplicate results",
        default: true
      },
      timeout: {
        type: "integer",
        description: "End-to-end timeout in seconds (default and maximum 30)",
        minimum: 1,
        maximum: 30,
        default: 30
      },
      ranking: {
        type: "string",
        description: "Ranking mode: default or provider",
        default: "default"
      }
    },
    required: ["query"],
    additionalProperties: false
  }
};

async function handleSearch(params) {
  const endpoint = `${apiUrl}/api/v1/search`;
  const controller = new AbortController();
  const timeoutSec = params.timeout || 30;
  const timeoutId = setTimeout(() => controller.abort(), timeoutSec * 1000);

  try {
    const res = await fetch(endpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": `Bearer ${apiKey}`
      },
      body: JSON.stringify(params),
      signal: controller.signal
    });

    clearTimeout(timeoutId);
    const data = await res.json();

    if (!res.ok) {
      return {
        isError: true,
        content: [
          {
            type: "text",
            text: JSON.stringify(data, null, 2)
          }
        ]
      };
    }

    return {
      content: [
        {
          type: "text",
          text: JSON.stringify(data, null, 2)
        }
      ]
    };
  } catch (err) {
    clearTimeout(timeoutId);
    return {
      isError: true,
      content: [
        {
          type: "text",
          text: `Search error: ${err.message || String(err)}`
        }
      ]
    };
  }
}

function sendResponse(response) {
  process.stdout.write(JSON.stringify(response) + "\n");
}

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

rl.on("line", async (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;

  let message;
  try {
    message = JSON.parse(trimmed);
  } catch (e) {
    return;
  }

  const { id, method, params } = message;

  if (method === "initialize") {
    sendResponse({
      jsonrpc: "2.0",
      id,
      result: {
        protocolVersion: PROTOCOL_VERSION,
        capabilities: {
          tools: {}
        },
        serverInfo: {
          name: "annolux",
          version: VERSION
        }
      }
    });
  } else if (method === "notifications/initialized") {
    // No response required for notifications
  } else if (method === "ping") {
    sendResponse({
      jsonrpc: "2.0",
      id,
      result: {}
    });
  } else if (method === "tools/list") {
    sendResponse({
      jsonrpc: "2.0",
      id,
      result: {
        tools: [TOOL_DEFINITION]
      }
    });
  } else if (method === "tools/call") {
    const toolName = params?.name;
    const toolArgs = params?.arguments || {};

    if (toolName === "search_web") {
      const result = await handleSearch(toolArgs);
      sendResponse({
        jsonrpc: "2.0",
        id,
        result
      });
    } else {
      sendResponse({
        jsonrpc: "2.0",
        id,
        error: {
          code: -32601,
          message: `Unknown tool: ${toolName}`
        }
      });
    }
  } else if (id !== undefined) {
    sendResponse({
      jsonrpc: "2.0",
      id,
      error: {
        code: -32601,
        message: `Method not found: ${method}`
      }
    });
  }
});
