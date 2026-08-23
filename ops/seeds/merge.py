#!/usr/bin/env python3
"""Merge per-vertical seed lists into one crawler seeds file.

Seeds are organized as curated host lists per vertical (ops/seeds/*.json).
The index itself is never partitioned by vertical; these files exist so a
crawl batch can target one vertical ("deepen EV") or any union of them.

usage: merge.py out.json zh-gov zh-auto-ev ...   (or "all" for every list)
"""
import json, sys, glob, os
here = os.path.dirname(os.path.abspath(__file__))
if len(sys.argv) < 3:
    sys.exit(__doc__)
out, names = sys.argv[1], sys.argv[2:]
if names == ["all"]:
    names = sorted(os.path.basename(p)[:-5] for p in glob.glob(f"{here}/*.json"))
seen, merged = set(), []
for n in names:
    for u in json.load(open(f"{here}/{n}.json")):
        if u not in seen:
            seen.add(u); merged.append(u)
json.dump(merged, open(out, "w"), indent=1)
print(f"{out}: {len(merged)} seeds from {len(names)} lists: {', '.join(names)}")
