#!/usr/bin/env bash
# 性能回归压测脚本（wrk）。
#
# 为什么存在
# ----------
# `docs/` 里的性能需求（静态预压缩、鉴权缓存）都有明确的 RPS 目标，改动前后
# 需要在同一台机器上跑同一组场景，否则「优化了」只能靠感觉。把三个代表性路径
# 固化下来，避免每次手敲 wrk 参数导致口径不一致。
#
# 三个场景分别对应三类开销：
#   1. /health              —— 纯框架开销（中间件、路由、日志）
#   2. 静态资源（CSS/JS）    —— 静态服务路径，考验预压缩是否生效
#   3. 鉴权路径             —— 会话校验 / token 校验，考验鉴权缓存
#
# 用法：
#   tools/bench.sh                          # 默认 http://127.0.0.1:3000
#   BASE_URL=http://127.0.0.1:3998 tools/bench.sh
#   SID=<sid cookie> TOKEN=<bearer> tools/bench.sh   # 带上凭据测鉴权路径
#   CONCURRENCY=50 DURATION=30s tools/bench.sh
#
# 缺少 SID / TOKEN 时，鉴权场景退化为未认证请求（401 路径），
# 仍然覆盖「路由 + 鉴权判定」的开销，只是不覆盖缓存命中后的查询省略。

set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:3000}"
THREADS="${THREADS:-4}"
CONCURRENCY="${CONCURRENCY:-50}"
DURATION="${DURATION:-20s}"
STATIC_PATH="${STATIC_PATH:-}"
SID="${SID:-}"
TOKEN="${TOKEN:-}"

if ! command -v wrk >/dev/null 2>&1; then
  echo "wrk not found. Install it (apt-get install wrk) or run the scenarios manually." >&2
  exit 1
fi

if ! curl -fsS -o /dev/null "${BASE_URL}/health"; then
  echo "backend is not reachable at ${BASE_URL}" >&2
  exit 1
fi

# 静态资源默认挑体积最大的 CSS：预压缩前后差异最明显。
if [ -z "$STATIC_PATH" ]; then
  STATIC_PATH="$(curl -fsS "${BASE_URL}/" 2>/dev/null | grep -oE '/assets/[^"]+\.css' | head -1 || true)"
fi
if [ -z "$STATIC_PATH" ]; then
  STATIC_PATH="/assets/index.css"
fi

header() {
  printf '\n=== %s ===\n' "$1"
}

run() {
  local label="$1"
  shift
  header "$label"
  # --latency 输出 P50/P90/P99；一行 Requests/sec 便于直接对比。
  wrk -t"$THREADS" -c"$CONCURRENCY" -d"$DURATION" --latency "$@" 2>&1 \
    | grep -E "Requests/sec|Latency  |Socket errors|Non-2xx"
}

header "baseline: ${BASE_URL} (threads=${THREADS} connections=${CONCURRENCY} duration=${DURATION})"

run "/health — framework only" "${BASE_URL}/health"

run "static ${STATIC_PATH} — gzip/br" \
  -H 'Accept-Encoding: gzip, deflate, br' "${BASE_URL}${STATIC_PATH}"

run "static ${STATIC_PATH} — identity (no Accept-Encoding)" \
  "${BASE_URL}${STATIC_PATH}"

if [ -n "$SID" ]; then
  run "auth session — authenticated (sid)" \
    -H "Cookie: sid=${SID}" "${BASE_URL}/api/auth/session"
else
  run "auth session — unauthenticated (401 path)" "${BASE_URL}/api/auth/session"
fi

if [ -n "$TOKEN" ]; then
  run "aiagent preference read — Bearer token" \
    -H "Authorization: Bearer ${TOKEN}" "${BASE_URL}/api/aiagent/preferences"
else
  run "aiagent — unauthenticated (401 path)" "${BASE_URL}/api/aiagent/preferences"
fi

printf '\nDone. Compare Requests/sec against the numbers in the performance requirements doc.\n'
