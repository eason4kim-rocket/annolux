#!/usr/bin/env bash
set -euo pipefail

workspace_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$workspace_root"

command -v pyftsubset >/dev/null || {
  echo 'pyftsubset is required (fonttools with Brotli support)' >&2
  exit 1
}
python3 -c 'import brotli' >/dev/null || {
  echo 'the Python Brotli package is required to decode WOFF2' >&2
  exit 1
}
python3 -c 'import fontTools' >/dev/null || {
  echo 'the Python fonttools package is required' >&2
  exit 1
}
test -f website/dist/zh/index.html || {
  echo 'generate website/dist with go run ./tools/sitegen first' >&2
  exit 1
}
: "${ANNOLUX_NOTO_SANS_SOURCE:?set ANNOLUX_NOTO_SANS_SOURCE to the untracked Noto Sans SC variable TTF}"
: "${ANNOLUX_NOTO_SERIF_SOURCE:?set ANNOLUX_NOTO_SERIF_SOURCE to the untracked Noto Serif SC variable TTF}"
test -f "$ANNOLUX_NOTO_SANS_SOURCE"
test -f "$ANNOLUX_NOTO_SERIF_SOURCE"

site_sans_chars="$(python3 -c 'import pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; files=list(pathlib.Path("website/dist/zh").rglob("*.html"))+list(pathlib.Path("website").glob("*.js")); s="".join(p.read_text(encoding="utf-8") for p in files); print("".join(sorted(set(re.findall("["+ranges+"]",s)))))')"
site_serif_chars="$(python3 -c 'import html,pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; s="".join(p.read_text(encoding="utf-8") for p in pathlib.Path("website/dist/zh").rglob("*.html")); parts=re.findall(r"<(?:h1|h2|em)(?: [^>]*)?>(.*?)</(?:h1|h2|em)>",s,flags=re.S|re.I); t=re.sub(r"<[^>]+>","",html.unescape("".join(parts))); print("".join(sorted(set(re.findall("["+ranges+"]",t)))))')"
home_sans_chars="$(python3 -c 'import pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; files=[pathlib.Path("website/dist/zh/index.html"),pathlib.Path("website/app.js")]; s="".join(p.read_text(encoding="utf-8") for p in files); print("".join(sorted(set(re.findall("["+ranges+"]",s)))))')"
home_serif_chars="$(python3 -c 'import html,re,sys; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; s=open(sys.argv[1],encoding="utf-8").read(); parts=re.findall(r"<em class=\"serif-accent\">(.*?)</em>",s,flags=re.S); t=re.sub(r"<[^>]+>","",html.unescape("".join(parts))); print("".join(sorted(set(re.findall("["+ranges+"]",t)))))' website/dist/zh/index.html)"

printf '%s\n' "$site_sans_chars" > website/assets/fonts/site-zh-chars.txt
printf '%s\n' "$site_serif_chars" > website/assets/fonts/site-zh-serif-chars.txt
printf '%s\n' "$home_sans_chars" > website/assets/fonts/home-zh-chars.txt
printf '%s\n' "$home_serif_chars" > website/assets/fonts/home-zh-serif-chars.txt

subset_options=(
  --flavor=woff2
  '--layout-features=*'
  --glyph-names
  --symbol-cmap
  --legacy-cmap
  --notdef-glyph
  --notdef-outline
  --recommended-glyphs
  '--name-IDs=*'
  --name-legacy
  '--name-languages=*'
)

pyftsubset "$ANNOLUX_NOTO_SANS_SOURCE" \
  --text="$site_sans_chars" \
  --output-file=website/assets/fonts/NotoSansSC-AnnoluxSubset.woff2 \
  "${subset_options[@]}"
pyftsubset "$ANNOLUX_NOTO_SERIF_SOURCE" \
  --text="$site_serif_chars" \
  --output-file=website/assets/fonts/NotoSerifSC-AnnoluxSubset.woff2 \
  "${subset_options[@]}"
pyftsubset "$ANNOLUX_NOTO_SANS_SOURCE" \
  --text="$home_sans_chars" \
  --output-file=website/assets/fonts/NotoSansSC-AnnoluxHome.woff2 \
  "${subset_options[@]}"
pyftsubset "$ANNOLUX_NOTO_SERIF_SOURCE" \
  --text="$home_serif_chars" \
  --output-file=website/assets/fonts/NotoSerifSC-AnnoluxHome.woff2 \
  "${subset_options[@]}"

python3 scripts/verify-font-subsets.py
if command -v sha256sum >/dev/null; then
  sha256sum website/assets/fonts/NotoSansSC-AnnoluxSubset.woff2 \
    website/assets/fonts/NotoSerifSC-AnnoluxSubset.woff2 \
    website/assets/fonts/NotoSansSC-AnnoluxHome.woff2 \
    website/assets/fonts/NotoSerifSC-AnnoluxHome.woff2 > website/assets/fonts/font-subsets.sha256
else
  shasum -a 256 website/assets/fonts/NotoSansSC-AnnoluxSubset.woff2 \
    website/assets/fonts/NotoSerifSC-AnnoluxSubset.woff2 \
    website/assets/fonts/NotoSansSC-AnnoluxHome.woff2 \
    website/assets/fonts/NotoSerifSC-AnnoluxHome.woff2 > website/assets/fonts/font-subsets.sha256
fi

echo 'Chinese font subsets: updated and verified'
