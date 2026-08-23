#!/usr/bin/env python3
"""Verify that committed Annolux Chinese font subsets cover their manifests."""

from pathlib import Path

from fontTools.ttLib import TTFont


ROOT = Path(__file__).resolve().parents[1]
FONT_ROOT = ROOT / "website" / "assets" / "fonts"
PAIRS = (
    ("NotoSansSC-AnnoluxSubset.woff2", "site-zh-chars.txt"),
    ("NotoSerifSC-AnnoluxSubset.woff2", "site-zh-serif-chars.txt"),
    ("NotoSansSC-AnnoluxHome.woff2", "home-zh-chars.txt"),
    ("NotoSerifSC-AnnoluxHome.woff2", "home-zh-serif-chars.txt"),
)


for font_name, manifest_name in PAIRS:
    expected = set((FONT_ROOT / manifest_name).read_text(encoding="utf-8").strip())
    cmap = TTFont(FONT_ROOT / font_name).getBestCmap()
    missing = sorted(character for character in expected if ord(character) not in cmap)
    if missing:
        raise SystemExit(f"{font_name} is missing: {''.join(missing)}")

print("Chinese font coverage: pass")
