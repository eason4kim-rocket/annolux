#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

go run ./tools/sitegen --check

search_lines() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -n -- "$pattern" "$@"
  else
    grep -REn -- "$pattern" "$@"
  fi
}

search_quiet() {
  local pattern="$1"
  shift
  if command -v rg >/dev/null 2>&1; then
    rg -q -- "$pattern" "$@"
  else
    grep -REq -- "$pattern" "$@"
  fi
}
test -f website/dist/index.html
test -f website/dist/zh/index.html
test -f website/dist/openapi.json
test -f website/dist/blog/index.html
test -f website/dist/zh/blog/index.html
test -f website/dist/assets/fonts/NotoSansSC-AnnoluxHome.woff2
test -f website/dist/assets/fonts/NotoSerifSC-AnnoluxHome.woff2
test -f website/dist/theme-init.js

for page_file in website/dist/index.html website/dist/zh/index.html website/dist/login/index.html website/dist/zh/login/index.html website/dist/docs/index.html website/dist/zh/docs/index.html; do
  search_quiet '<script src="/theme-init\.js(\?v=[a-z0-9]+)?"></script>' "$page_file"
done
if search_lines '<script>[[:space:]]*$' website/dist; then
  echo 'inline executable script leaked into the generated site' >&2
  exit 1
fi
search_quiet '^\.auth-legal[[:space:]]*\{' website/dist/auth.css
search_quiet '^\.auth-legal a[[:space:]]*\{' website/dist/auth.css

rendered_site_zh_chars="$(python3 -c 'import pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; files=list(pathlib.Path("website/dist/zh").rglob("*.html"))+list(pathlib.Path("website").glob("*.js")); s="".join(p.read_text(encoding="utf-8") for p in files); print("".join(sorted(set(re.findall("["+ranges+"]",s)))))')"
recorded_site_zh_chars="$(tr -d '\r\n' < website/assets/fonts/site-zh-chars.txt)"
test "$rendered_site_zh_chars" = "$recorded_site_zh_chars" || {
  echo 'Chinese site text changed; regenerate its reviewed font subsets' >&2
  exit 1
}
rendered_site_zh_serif_chars="$(python3 -c 'import html,pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; s="".join(p.read_text(encoding="utf-8") for p in pathlib.Path("website/dist/zh").rglob("*.html")); parts=re.findall(r"<(?:h1|h2|em)(?: [^>]*)?>(.*?)</(?:h1|h2|em)>",s,flags=re.S|re.I); t=re.sub(r"<[^>]+>","",html.unescape("".join(parts))); print("".join(sorted(set(re.findall("["+ranges+"]",t)))))')"
recorded_site_zh_serif_chars="$(tr -d '\r\n' < website/assets/fonts/site-zh-serif-chars.txt)"
test "$rendered_site_zh_serif_chars" = "$recorded_site_zh_serif_chars" || {
  echo 'Chinese headings changed; regenerate their reviewed font subset' >&2
  exit 1
}

rendered_zh_chars="$(python3 -c 'import pathlib,re; ranges="\\u2e80-\\u2eff\\u2f00-\\u2fdf\\u3000-\\u303f\\u31c0-\\u31ef\\u3400-\\u4dbf\\u4e00-\\u9fff\\uf900-\\ufaff\\ufe30-\\ufe4f\\uff00-\\uffef"; files=[pathlib.Path("website/dist/zh/index.html"),pathlib.Path("website/app.js")]; s="".join(p.read_text(encoding="utf-8") for p in files); print("".join(sorted(set(re.findall("["+ranges+"]",s)))))')"
recorded_zh_chars="$(tr -d '\r\n' < website/assets/fonts/home-zh-chars.txt)"
test "$rendered_zh_chars" = "$recorded_zh_chars" || {
  echo 'Chinese homepage text changed; regenerate its reviewed font subset' >&2
  exit 1
}
rendered_zh_serif_chars="$(python3 -c 'import html,re,sys; s=open(sys.argv[1],encoding="utf-8").read(); parts=re.findall(r"<em class=\"serif-accent\">(.*?)</em>",s,flags=re.S); print("".join(sorted(set(re.sub(r"<[^>]+>","",html.unescape("".join(parts)))))))' website/dist/zh/index.html)"
recorded_zh_serif_chars="$(tr -d '\r\n' < website/assets/fonts/home-zh-serif-chars.txt)"
test "$rendered_zh_serif_chars" = "$recorded_zh_serif_chars" || {
  echo 'Chinese serif accent changed; regenerate its reviewed font subset' >&2
  exit 1
}

if command -v sha256sum >/dev/null; then
  sha256sum -c website/assets/fonts/font-subsets.sha256 >/dev/null
else
  shasum -a 256 -c website/assets/fonts/font-subsets.sha256 >/dev/null
fi

if search_lines '<a class="language-toggle"[^>]*aria-label=' website/dist/index.html website/dist/zh/index.html; then
  echo 'homepage language link overrides its visible accessible name' >&2
  exit 1
fi

if search_lines '(\?lang=zh|href="[^"]+\.html|canonical" href="[^"]+\.html)' website/dist; then
  echo 'legacy language or .html URL leaked into generated site' >&2
  exit 1
fi

for page_file in \
  website/dist/account/index.html website/dist/playground/index.html website/dist/checkout/success/index.html website/dist/billing/checkout/index.html website/dist/login/index.html \
  website/dist/zh/account/index.html website/dist/zh/playground/index.html website/dist/zh/checkout/success/index.html website/dist/zh/billing/checkout/index.html website/dist/zh/login/index.html; do
  search_quiet '<meta name="robots" content="noindex,nofollow,noarchive"' "$page_file"
  search_quiet '<meta http-equiv="Cache-Control" content="no-store"' "$page_file"
done
search_quiet 'Annolux is operated by Eason Liu' website/dist/refund/index.html
search_quiet 'Annolux 由 Eason Liu 运营' website/dist/zh/refund/index.html

for page_file in website/dist/blog/*/index.html website/dist/zh/blog/*/index.html; do
  search_quiet '"@type":"BlogPosting"' "$page_file"
  search_quiet '"@type":"BreadcrumbList"' "$page_file"
done

proof_file="$(python3 -c 'import pathlib,re; files=[p for p in pathlib.Path("website/dist").glob("*.txt") if re.fullmatch(r"[0-9a-f]{64}\.txt",p.name)]; assert len(files)==1; print(files[0])')"
indexnow_key="$(tr -d '\r\n' < "$proof_file")"
test "$indexnow_key" = "$(basename "$proof_file" .txt)"
[[ "$indexnow_key" =~ ^[0-9a-f]{64}$ ]]
search_quiet '<rss version="2.0"' website/dist/feed.xml

# Plan prices and credit allowances live on the pricing page, which every page
# links from its header. The home page is a landing page and carries no prices.
for page_file in website/dist/pricing/index.html website/dist/zh/pricing/index.html; do
  search_quiet '\$0' "$page_file"
  search_quiet '\$29' "$page_file"
  search_quiet '\$99' "$page_file"
  search_quiet '20,000' "$page_file"
  search_quiet '100,000' "$page_file"
done

# Pricing must stay reachable from the home page, in both locales.
search_quiet 'href="/pricing"' website/dist/index.html
search_quiet 'href="/zh/pricing"' website/dist/zh/index.html

search_quiet 'href="/billing/checkout\?plan=pro"' website/dist/pricing/index.html
search_quiet 'href="/billing/checkout\?plan=scale"' website/dist/pricing/index.html
for page_file in website/dist/zh/pricing/index.html; do
  search_quiet 'href="/zh/billing/checkout\?plan=pro"' "$page_file"
  search_quiet 'href="/zh/billing/checkout\?plan=scale"' "$page_file"
done
if search_lines '(Paddle checkout pending|Paddle 结账待上线|github\.com/[^" ]+/issues)' website/dist/index.html website/dist/pricing/index.html website/dist/zh/index.html website/dist/zh/pricing/index.html; then
  echo 'legacy paid-plan pending CTA leaked into the generated site' >&2
  exit 1
fi
search_quiet 'data-language-link="" href="/login"' website/dist/zh/login/index.html
if search_lines '/zh/zh/' website/dist; then
  echo 'duplicated Chinese route prefix leaked into the generated site' >&2
  exit 1
fi

count=0
while IFS= read -r -d '' page_file; do
  if search_quiet 'rel="canonical"' "$page_file"; then
    count=$((count + 1))
  fi
done < <(find website/dist -name index.html -print0)
total="$(find website/dist -name index.html | wc -l | tr -d ' ')"
test "$count" = "$total"

jq -e '.openapi == "3.1.0" and .info.title == "Annolux Search API"' website/dist/openapi.json >/dev/null
echo 'static site: pass'
