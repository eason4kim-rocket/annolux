"""
Annolux Example 01: Automated Technical Survey Agent with Timestamp Provenance

Demonstrates how an AI agent can query the Annolux curated bilingual index,
inspect `fetched_at` timestamps to avoid outdated blog posts, and format
evidence-backed research citations for LLM synthesis.
"""

import os
import sys
import json
import requests
from datetime import datetime

ANNOLUX_API_KEY = os.environ.get("ANNOLUX_API_KEY", "")
ANNOLUX_API_URL = os.environ.get("ANNOLUX_API_URL", "https://api.annolux.com/api/v1/search")

def search_annolux(query: str, domains: list = None, limit: int = 5) -> dict:
    """Execute a search query against the Annolux curated index."""
    if not ANNOLUX_API_KEY:
        print("⚠️ Warning: ANNOLUX_API_KEY environment variable is not set.", file=sys.stderr)
        print("Get 1,000 free credits at https://annolux.com", file=sys.stderr)
        sys.exit(1)

    headers = {
        "Authorization": f"Bearer {ANNOLUX_API_KEY}",
        "Content-Type": "application/json"
    }
    payload = {
        "query": query,
        "limit": limit,
        "deduplicate": True
    }
    if domains:
        payload["domains"] = domains

    resp = requests.post(ANNOLUX_API_URL, headers=headers, json=payload, timeout=15)
    resp.raise_for_status()
    return resp.json()

def generate_technical_survey(topic: str, domains: list = None) -> str:
    """Gather time-stamped evidence and format a research briefing."""
    print(f"🔍 Searching Annolux curated corpus for: '{topic}'...")
    data = search_annolux(query=topic, domains=domains, limit=3)
    results = data.get("results", [])

    if not results:
        return f"No results found in the curated index for '{topic}'."

    citations = []
    for item in results:
        fetch_date = item.get("fetched_at", "Unknown")[:10]
        title = item.get("title", "Untitled")
        url = item.get("url", "")
        snippet = item.get("snippet", "")
        citations.append(
            f"### [{fetch_date}] {title}\n"
            f"- **URL**: {url}\n"
            f"- **Snapshot Time**: `{item.get('fetched_at')}`\n"
            f"- **Summary**: {snippet}\n"
        )

    briefing = (
        f"# Technical Survey: {topic}\n"
        f"*Generated: {datetime.utcnow().strftime('%Y-%m-%d %H:%M:%SZ')} via Annolux Search*\n\n"
        f"## Gathered Citations ({len(results)} sources)\n\n"
        + "\n".join(citations)
    )
    return briefing

if __name__ == "__main__":
    topic = sys.argv[1] if len(sys.argv) > 1 else "Rust 2024 async fn in trait AFIT"
    report = generate_technical_survey(topic)
    print("\n" + report)
