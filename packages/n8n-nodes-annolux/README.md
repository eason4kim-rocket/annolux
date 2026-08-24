# n8n-nodes-annolux

Official [n8n](https://n8n.io) community node for **[Annolux](https://annolux.com)** — The curated English & Chinese web search API and MCP for AI Agents with explicit snapshot timestamps.

[![npm version](https://img.shields.io/npm/v/n8n-nodes-annolux.svg?style=flat-square)](https://www.npmjs.com/package/n8n-nodes-annolux)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=flat-square)](https://opensource.org/licenses/MIT)

---

## ⚡ Features

- 🔍 **Curated English & Chinese Corpus**: High-purity technical and knowledge corpus with zero SEO spam.
- 🕒 **Explicit `fetched_at` Provenance**: Every search result contains verifiable snapshot timestamps to eliminate LLM hallucinations.
- 🤖 **Native n8n AI Agent Tool**: Attach Annolux directly to n8n AI Agent nodes to empower models (OpenAI, Anthropic, Mistral, Ollama) with real-time web retrieval.
- ⚙️ **Fine-Grained Controls**: Support for `query`, `limit`, `domains` whitelist filtering, `deduplicate`, and `ranking` strategy.

---

## 📦 Installation

In your self-hosted or cloud n8n instance:

1. Go to **Settings** > **Community Nodes** > **Install a community node**.
2. Enter `n8n-nodes-annolux`.
3. Agree to the terms and click **Install**.

---

## 🔑 Credentials

1. Sign up for free at [annolux.com](https://annolux.com) to get your API key (1,000 free requests/month).
2. In n8n, create a new credential for **Annolux API** and paste your API key (`ann_live_...` or `ann_test_...`).

---

## 🚀 Usage

### Mode 1: Connect to n8n AI Agent (Recommended)
1. Add an **AI Agent** node in n8n.
2. Drag **Annolux Search** and connect it to the **Tools** input of the AI Agent.
3. The AI Agent will now autonomously decide when to execute web search to answer user queries with up-to-date facts.

### Mode 2: Standard Workflow Execution
1. Add the **Annolux Search** node anywhere in your workflow.
2. Set the `query` expression (e.g. `={{ $json.searchTerm }}`).
3. Connect the output to downstream nodes.

---

## 📄 License

MIT © [Annolux](https://annolux.com)
