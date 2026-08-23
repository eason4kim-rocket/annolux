#!/usr/bin/env python3
"""Generates the frozen v3 corpus: cases.jsonl, manifest.json, release-gate.json.

Case design: "core" targets are URLs verified present in the production index
(the v2 target list plus session-verified pages); "head" targets are canonical
pages of the head-coverage seed sites, which measure what fusion over the
crawled discovery tier adds. Both splits carry the same mix.
"""
import hashlib
import json

C, H = "core-v3", "head-coverage"


def case(id_, lang, query, targets, kind, category="technical"):
    return {
        "id": id_, "language": lang, "query": query, "category": category,
        "targets": [{"url": u, "relevance": r} for u, r in targets], "kind": kind,
    }


CASES = [
    # ---- core: verified-in-index targets (en) ----
    case("k8s-pod-liveness-readiness", "en", "kubernetes liveness and readiness probes",
         [("https://kubernetes.io/docs/concepts/workloads/pods/probes/", 3),
          ("https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/", 2)], C),
    case("k8s-configmap-usage", "en", "kubernetes configmap configuration data",
         [("https://kubernetes.io/docs/concepts/configuration/configmap/", 3)], C),
    case("k8s-secret-management", "en", "kubernetes secrets sensitive data",
         [("https://kubernetes.io/docs/concepts/configuration/secret/", 3)], C),
    case("k8s-service-types", "en", "kubernetes service clusterip nodeport",
         [("https://kubernetes.io/docs/concepts/services-networking/service/", 3)], C),
    case("docker-multi-stage-builds", "en", "docker multi stage build smaller images",
         [("https://docs.docker.com/build/building/multi-stage/", 3),
          ("https://docs.docker.com/get-started/docker-concepts/building-images/multi-stage-builds/", 2)], C),
    case("docker-volumes-persistence", "en", "docker volumes persist container data",
         [("https://docs.docker.com/engine/storage/volumes/", 3)], C),
    case("docker-bind-mounts", "en", "docker bind mount host directory",
         [("https://docs.docker.com/engine/storage/bind-mounts/", 3)], C),
    case("dockerfile-reference-instructions", "en", "dockerfile instructions reference",
         [("https://docs.docker.com/reference/dockerfile/", 3)], C, "reference"),
    case("python-pathlib-paths", "en", "python pathlib object oriented filesystem paths",
         [("http://docs.python.org/3/library/pathlib.html", 3),
          ("http://docs.python.org/library/pathlib.html", 2)], C, "reference"),
    case("python-dataclasses-usage", "en", "python dataclasses decorator fields",
         [("http://docs.python.org/3/library/dataclasses.html", 3),
          ("http://docs.python.org/library/dataclasses.html", 2)], C, "reference"),
    case("python-asyncio-concurrency", "en", "python asyncio coroutines event loop",
         [("http://docs.python.org/3/library/asyncio.html", 3),
          ("http://docs.python.org/library/asyncio.html", 2)], C, "reference"),
    case("python-argparse-cli", "en", "python argparse command line arguments",
         [("http://docs.python.org/3/howto/argparse.html", 3),
          ("http://docs.python.org/howto/argparse.html", 2)], C),
    case("python-exception-handling", "en", "python try except exception handling tutorial",
         [("http://docs.python.org/3/tutorial/errors.html", 3),
          ("http://docs.python.org/tutorial/errors.html", 2)], C),
    case("rust-error-handling-result", "en", "rust error handling result panic",
         [("https://doc.rust-lang.org/book/ch09-00-error-handling.html", 3)], C),
    case("rust-traits-shared-behavior", "en", "rust traits defining shared behavior",
         [("https://doc.rust-lang.org/book/ch10-02-traits.html", 3)], C),
    case("rust-iterators-lazy", "en", "rust iterators processing items",
         [("https://doc.rust-lang.org/book/ch13-02-iterators.html", 3)], C),
    case("rust-smart-pointers-box", "en", "rust smart pointers box rc refcell",
         [("https://doc.rust-lang.org/book/ch15-00-smart-pointers.html", 3)], C),
    case("cargo-dependencies", "en", "cargo add dependencies crates",
         [("https://doc.rust-lang.org/cargo/guide/dependencies.html", 3)], C),
    case("go-effective-code", "en", "effective go idiomatic code",
         [("https://go.dev/doc/effective_go", 3)], C),
    case("go-slices-internals", "en", "go slices usage internals append",
         [("https://go.dev/blog/slices-intro", 3),
          ("https://go.dev/doc/articles/slices_usage_and_internals.html", 2)], C),
    case("go-modules-dependencies", "en", "go modules managing dependencies",
         [("https://go.dev/doc/modules/managing-dependencies", 3)], C),
    case("go-fuzzing-tests", "en", "go fuzzing random test inputs",
         [("https://go.dev/doc/fuzz", 3)], C),
    case("node-event-loop-timers", "en", "nodejs event loop timers nexttick",
         [("https://nodejs.org/learn/asynchronous-work/event-loop-timers-and-nexttick", 3)], C),
    case("node-streams-data", "en", "nodejs streams reading writing data",
         [("https://nodejs.org/learn/modules/how-to-use-streams", 3)], C),
    case("node-event-emitter", "en", "nodejs event emitter custom events",
         [("https://nodejs.org/learn/asynchronous-work/the-nodejs-event-emitter", 3)], C),
    case("postgres-create-index", "en", "postgresql create index syntax",
         [("https://www.postgresql.org/docs/current/sql-createindex.html", 3)], C, "reference"),
    case("postgres-full-text-search", "en", "postgresql full text search tsvector",
         [("https://www.postgresql.org/docs/current/textsearch.html", 3)], C, "reference"),
    case("php-exceptions", "en", "php exceptions try catch finally",
         [("https://www.php.net/manual/en/language.exceptions.php", 3)], C, "reference"),
    case("php-namespaces", "en", "php namespaces organizing classes",
         [("https://www.php.net/manual/en/language.namespaces.php", 3)], C, "reference"),
    case("mdn-fetch-api", "en", "javascript fetch api network requests",
         [("https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API", 3),
          ("https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch", 2)], C, "reference"),
    case("mdn-css-grid-layout", "en", "css grid layout two dimensional",
         [("https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout", 3)], C, "reference"),
    case("mdn-flexbox-basics", "en", "css flexbox basic concepts axis",
         [("https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Flexible_box_layout/Basic_concepts", 3)], C, "reference"),
    case("linux-cfs-scheduler", "en", "linux completely fair scheduler design",
         [("https://docs.kernel.org/scheduler/sched-design-CFS.html", 3)], C, "reference"),
    case("arch-installation-guide", "en", "arch linux installation guide",
         [("https://wiki.archlinux.org/title/Installation_guide", 3)], C),
    case("ubuntu-ufw-firewall", "en", "ubuntu server firewall ufw setup",
         [("https://ubuntu.com/server/docs/how-to/security/firewalls/", 3)], C),
    case("android-activity-lifecycle", "en", "android activity lifecycle callbacks",
         [("http://d.android.com/guide/components/activities/activity-lifecycle", 3)], C, "reference"),
    case("android-compose-state", "en", "jetpack compose state management",
         [("http://d.android.com/develop/ui/compose/state", 3),
          ("http://d.android.com/jetpack/compose/state", 2)], C, "reference"),
    case("rfc-http-semantics", "en", "http semantics rfc 7231",
         [("https://rfc-editor.org/info/rfc7231/", 3)], C, "reference"),
    case("rfc-tls13", "en", "tls 1.3 protocol rfc",
         [("https://rfc-editor.org/info/rfc8446/", 3)], C, "reference"),
    case("rfc-uri-syntax", "en", "uri generic syntax rfc 3986",
         [("https://rfc-editor.org/info/rfc3986", 3)], C, "reference"),

    # ---- core: verified-in-index targets (zh) ----
    case("git-branch-zh", "zh", "git 分支 创建 合并",
         [("http://www.runoob.com/git/git-branch.html", 3),
          ("https://www.ruanyifeng.com/blog/2012/07/git.html", 2)], C),
    case("http-status-codes-zh", "zh", "HTTP 状态码 含义",
         [("http://www.runoob.com/http/http-status-codes.html", 3)], C, "reference"),
    case("python-datastructures-zh", "zh", "Python 列表 数据结构 教程",
         [("https://docs.python.org/zh-cn/3/tutorial/datastructures.html", 3)], C),
    case("k8s-pod-lifecycle-zh", "zh", "Kubernetes Pod 生命周期",
         [("https://kubernetes.io/zh-cn/docs/concepts/workloads/pods/pod-lifecycle/", 3)], C),
    case("k8s-taint-toleration-zh", "zh", "Kubernetes 污点 容忍度 调度",
         [("https://kubernetes.io/zh-cn/docs/concepts/scheduling-eviction/taint-and-toleration/", 3)], C),
    case("css-grid-zh", "zh", "CSS 网格布局 教程",
         [("http://www.runoob.com/css3/css-grid.html", 3),
          ("http://www.runoob.com/css/css-grid.html", 2)], C),
    case("flex-layout-zh", "zh", "Flex 弹性布局 语法",
         [("https://www.ruanyifeng.com/blog/2015/07/flex-grammar.html", 3)], C),
    case("js-promise-zh", "zh", "JavaScript Promise 异步 用法",
         [("http://javascript.ruanyifeng.com/advanced/promise.html", 3)], C),
    case("java-thread-zh", "zh", "Java 多线程 创建方式",
         [("http://www.runoob.com/java/java-multithreading.html", 3)], C),
    case("sql-drop-table-zh", "zh", "SQL 删除表 DROP 语句",
         [("http://www.runoob.com/sql/sql-drop.html", 3)], C, "reference"),
    case("cpp-template-zh", "zh", "C++ 模板 泛型编程",
         [("http://www.runoob.com/cplusplus/cpp-templates.html", 3)], C),
    case("django-template-zh", "zh", "Django 模板 语法",
         [("http://www.runoob.com/django/django-template.html", 3)], C),

    # ---- core: mixed-script queries against verified targets ----
    case("k8s-ingress-zh-mixed", "mixed", "Kubernetes Ingress 配置 路由",
         [("https://kubernetes.io/zh-cn/docs/concepts/services-networking/ingress/", 3),
          ("https://kubernetes.io/zh-cn/docs/concepts/services-networking/ingress-controllers/", 2)], C),
    case("hpa-autoscale-zh-mixed", "mixed", "Kubernetes HPA 自动扩缩容",
         [("https://kubernetes.io/zh-cn/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/", 3)], C),
    case("prototype-pattern-zh-mixed", "mixed", "JavaScript prototype 原型链",
         [("http://www.runoob.com/js/js-object-prototype.html", 3)], C),

    # ---- head coverage: seed-site canonical targets (en) ----
    case("react-usestate-hook", "en", "react useState hook state variable",
         [("https://react.dev/reference/react/useState", 3)], H, "reference"),
    case("react-useeffect-cleanup", "en", "react useEffect cleanup dependencies",
         [("https://react.dev/reference/react/useEffect", 3)], H, "reference"),
    case("react-thinking-in-react", "en", "thinking in react component hierarchy",
         [("https://react.dev/learn/thinking-in-react", 3)], H),
    case("vue-computed-properties", "en", "vue computed properties reactive",
         [("https://vuejs.org/guide/essentials/computed.html", 3)], H),
    case("vue-component-props", "en", "vue component props declaration",
         [("https://vuejs.org/guide/components/props.html", 3)], H),
    case("nextjs-app-router", "en", "nextjs app router layouts pages",
         [("https://nextjs.org/docs/app", 3)], H, "reference"),
    case("typescript-generics-intro", "en", "typescript generics type parameters",
         [("https://www.typescriptlang.org/docs/handbook/2/generics.html", 3)], H, "reference"),
    case("typescript-utility-types", "en", "typescript utility types partial pick",
         [("https://www.typescriptlang.org/docs/handbook/utility-types.html", 3)], H, "reference"),
    case("tailwind-responsive-design", "en", "tailwind responsive design breakpoints",
         [("https://tailwindcss.com/docs/responsive-design", 3)], H, "reference"),
    case("vite-env-variables", "en", "vite environment variables modes",
         [("https://vite.dev/guide/env-and-mode", 3)], H),
    case("svelte-reactivity-runes", "en", "svelte reactivity state runes",
         [("https://svelte.dev/docs/svelte/what-are-runes", 3)], H, "reference"),
    case("astro-islands-architecture", "en", "astro islands partial hydration",
         [("https://docs.astro.build/en/concepts/islands/", 3)], H),
    case("express-routing-guide", "en", "express routing route parameters",
         [("https://expressjs.com/en/guide/routing.html", 3)], H, "reference"),
    case("nestjs-dependency-injection", "en", "nestjs providers dependency injection",
         [("https://docs.nestjs.com/providers", 3)], H, "reference"),
    case("fastapi-path-parameters", "en", "fastapi path parameters validation",
         [("https://fastapi.tiangolo.com/tutorial/path-params/", 3)], H),
    case("django-orm-queries", "en", "django orm making queries filter",
         [("https://docs.djangoproject.com/en/5.2/topics/db/queries/", 3)], H, "reference"),
    case("flask-application-factory", "en", "flask application factory pattern",
         [("https://flask.palletsprojects.com/en/stable/patterns/appfactories/", 3)], H),
    case("stripe-webhooks-setup", "en", "stripe webhooks receive events",
         [("https://docs.stripe.com/webhooks", 3)], H, "reference"),
    case("supabase-row-level-security", "en", "supabase row level security policies",
         [("https://supabase.com/docs/guides/database/postgres/row-level-security", 3)], H),
    case("cloudflare-workers-kv", "en", "cloudflare workers kv storage",
         [("https://developers.cloudflare.com/kv/", 3)], H, "reference"),
    case("deno-permissions-model", "en", "deno permissions security flags",
         [("https://docs.deno.com/runtime/fundamentals/security/", 3)], H),
    case("bun-package-manager", "en", "bun install package manager",
         [("https://bun.sh/docs/cli/install", 3)], H, "reference"),
    case("git-rebase-docs", "en", "git rebase reapply commits",
         [("https://git-scm.com/docs/git-rebase", 3)], H, "reference"),
    case("github-actions-workflow-syntax", "en", "github actions workflow syntax yaml",
         [("https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax", 3)], H, "reference"),
    case("redis-data-types", "en", "redis data types strings hashes",
         [("https://redis.io/docs/latest/develop/data-types/", 3)], H, "reference"),
    case("mongodb-aggregation-pipeline", "en", "mongodb aggregation pipeline stages",
         [("https://www.mongodb.com/docs/manual/core/aggregation-pipeline/", 3)], H, "reference"),
    case("prometheus-querying-promql", "en", "prometheus promql querying basics",
         [("https://prometheus.io/docs/prometheus/latest/querying/basics/", 3)], H, "reference"),
    case("terraform-state-management", "en", "terraform state backends remote",
         [("https://developer.hashicorp.com/terraform/language/state", 3)], H, "reference"),
    case("ansible-playbooks-intro", "en", "ansible playbooks tasks handlers",
         [("https://docs.ansible.com/ansible/latest/playbook_guide/playbooks_intro.html", 3)], H),
    case("grpc-core-concepts", "en", "grpc core concepts service methods",
         [("https://grpc.io/docs/what-is-grpc/core-concepts/", 3)], H),
    case("protobuf-language-guide", "en", "protocol buffers proto3 language guide",
         [("https://protobuf.dev/programming-guides/proto3/", 3)], H, "reference"),
    case("graphql-schema-types", "en", "graphql schema object types fields",
         [("https://graphql.org/learn/schema/", 3)], H),
    case("nginx-reverse-proxy", "en", "nginx reverse proxy configuration",
         [("https://nginx.org/en/docs/http/ngx_http_proxy_module.html", 3)], H, "reference"),
    case("numpy-broadcasting-rules", "en", "numpy broadcasting arrays rules",
         [("https://numpy.org/doc/stable/user/basics.broadcasting.html", 3)], H, "reference"),
    case("pandas-merge-join", "en", "pandas merge join dataframes",
         [("https://pandas.pydata.org/docs/user_guide/merging.html", 3)], H, "reference"),
    case("pytorch-autograd-intro", "en", "pytorch autograd automatic differentiation",
         [("https://pytorch.org/docs/stable/notes/autograd.html", 3)], H, "reference"),
    case("eslint-configure-rules", "en", "eslint configure rules config file",
         [("https://eslint.org/docs/latest/use/configure/rules", 3)], H, "reference"),
    case("jest-mock-functions", "en", "jest mock functions spies",
         [("https://jestjs.io/docs/mock-functions", 3)], H),
    case("kotlin-coroutines-basics", "en", "kotlin coroutines suspend functions",
         [("https://kotlinlang.org/docs/coroutines-basics.html", 3)], H),
    case("npm-package-json-reference", "en", "npm package json fields reference",
         [("https://docs.npmjs.com/cli/v11/configuring-npm/package-json", 3)], H, "reference"),
    case("pnpm-workspaces-monorepo", "en", "pnpm workspaces monorepo setup",
         [("https://pnpm.io/workspaces", 3)], H),
    case("mcp-server-concepts", "en", "model context protocol server concepts",
         [("https://modelcontextprotocol.io/docs/learn/server-concepts", 3)], H),

    # ---- head coverage (zh + mixed) ----
    case("vue-reactivity-zh", "zh", "Vue 响应式 基础 教程",
         [("https://vuejs.org/zh/guide/essentials/reactivity-fundamentals.html", 3)], H),
    case("react-usestate-zh-mixed", "mixed", "React useState 状态 用法",
         [("https://zh-hans.react.dev/reference/react/useState", 3)], H, "reference"),
    case("k8s-deployment-zh", "zh", "Kubernetes Deployment 滚动更新",
         [("https://kubernetes.io/zh-cn/docs/concepts/workloads/controllers/deployment/", 3)], H),
]

# split assignment: interleave core/head deterministically, 60 dev / 40 heldout.
core = [c for c in CASES if c["kind"] == C]
head = [c for c in CASES if c["kind"] == H]
assert len(CASES) == 100, f"cases={len(CASES)} (core={len(core)} head={len(head)})"

dev, heldout = [], []
for bucket in (core, head):
    for i, item in enumerate(bucket):
        (dev if i % 5 != 3 and i % 5 != 4 else heldout).append(item)
# rebalance to exactly 60/40
while len(dev) > 60:
    heldout.append(dev.pop())
while len(dev) < 60:
    dev.append(heldout.pop())
assert len(dev) == 60 and len(heldout) == 40

lines = []
counts = {"splits": {}, "languages": {}, "categories": {}, "tags": {}}
for split, items in (("dev", dev), ("heldout", heldout)):
    for item in items:
        tags = sorted({item["kind"]} | ({"blind-v3"} if split == "heldout" else set()))
        row = {
            "id": item["id"], "split": split, "language": item["language"],
            "category": item["category"], "query": item["query"],
            "targets": item["targets"], "tags": tags,
        }
        lines.append(json.dumps(row, ensure_ascii=False, separators=(",", ":")))
        counts["splits"][split] = counts["splits"].get(split, 0) + 1
        counts["languages"][item["language"]] = counts["languages"].get(item["language"], 0) + 1
        counts["categories"][item["category"]] = counts["categories"].get(item["category"], 0) + 1
        for tag in tags:
            counts["tags"][tag] = counts["tags"].get(tag, 0) + 1

cases_bytes = ("\n".join(lines) + "\n").encode()
sha = hashlib.sha256(cases_bytes).hexdigest()
lock_sha = hashlib.sha256(open("searchindex/ranking.lock.json", "rb").read()).hexdigest()

manifest = {
    "schema_version": 1,
    "name": "search-generalization-v3",
    "frozen_at": "2026-08-17",
    "cases_file": "cases.jsonl",
    "cases_sha256": sha,
    "expected": {"total": 100, **counts},
    "protocol": {
        "limit": 10, "request_interval_ms": 250,
        "strong_grade": 3, "relevant_grade": 2,
        "heldout_policy": "Freeze before the first run. Tune fusion only on dev; run heldout once at the fusion-enable gate. If the gate fails, heldout becomes exposed evidence and must never be used as a blind set again.",
    },
}
gate = {
    "schema_version": 1,
    "name": "search-generalization-v3-heldout-gate",
    "frozen_at": "2026-08-17",
    "corpus_name": "search-generalization-v3",
    "split": "heldout",
    "expected_cases": 40,
    "ranking_lock_sha256": lock_sha,
    "minimum": {"hit_at_3": 0.55, "hit_at_10": 0.72, "mrr_at_10": 0.45},
    "maximum": {"errors": 0, "client_p95_latency_ms": 1200},
    "failure_policy": "Do not tune fusion against this heldout split. A failed gate exposes v3 heldout; keep fusion disabled in production, move diagnosed cases into a future dev pool, and author a new blind split before another enable attempt.",
}

import os
os.makedirs("scripts/searcheval/corpus/v3", exist_ok=True)
open("scripts/searcheval/corpus/v3/cases.jsonl", "wb").write(cases_bytes)
open("scripts/searcheval/corpus/v3/manifest.json", "w").write(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
open("scripts/searcheval/corpus/v3/release-gate.json", "w").write(json.dumps(gate, ensure_ascii=False, indent=2) + "\n")
print("counts:", json.dumps(counts))
print("cases sha:", sha)
print("lock sha:", lock_sha)
