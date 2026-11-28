#!/usr/bin/env bash
# 本机开发用镜像构建：复用已发布的运行时镜像作为基底，只覆盖本机编译产物。
#
# 为什么不用仓库根 Dockerfile：它会从源码全量编译前端 + Go + 三平台 Agent，
# 本机没有 x86_64/windows 交叉工具链，跑一次要很久。开发调试只需要
# 「我改的代码进镜像」，用这个脚本几秒出结果。
#
# 用法：
#   ./tools/local-image.sh                 # 打 iwvw/api-monitor:local
#   TAG=dev ./tools/local-image.sh         # 覆盖 tag
#   RUNTIME_BASE=ghcr.io/iwvw/api-monitor:dev ./tools/local-image.sh
#
# 前置：musl 交叉工具链（apt install musl-tools）+ rustup target add aarch64-unknown-linux-musl
set -euo pipefail

cd "$(dirname "$0")/.."

TAG="${TAG:-local}"
IMAGE="iwvw/api-monitor:${TAG}"
RUNTIME_BASE="${RUNTIME_BASE:-iwvw/api-monitor:dev}"
ARCH="$(uname -m)"

case "$ARCH" in
  aarch64|arm64) GOARCH=arm64; RUST_TARGET=aarch64-unknown-linux-musl; AGENT_OUT=agent-linux-arm64 ;;
  x86_64|amd64)  GOARCH=amd64; RUST_TARGET=x86_64-unknown-linux-musl;  AGENT_OUT=agent-linux-amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

log() { printf '\033[1;36m[local-image]\033[0m %s\n' "$*"; }

export PATH="$PATH:/usr/local/go/bin:$HOME/.cargo/bin"

log "1/4 构建前端 (vite build)"
npx vite build >/dev/null

log "2/4 构建 Go 后端 (CGO_ENABLED=0 GOARCH=$GOARCH，静态)"
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
  go build -C backend-go -trimpath -ldflags="-s -w" -o api-monitor ./cmd/api-monitor

log "3/4 构建 Agent ($RUST_TARGET,仅本机平台)"
# Agent 已从正式镜像剥离:本地镜像只带本机平台的 Agent(供同平台调试)。
# 其它平台由本仓库 release 提供,面板按 AGENT_DOWNLOAD_URL 下载。
mkdir -p dist/agent
rm -f dist/agent/agent-linux-amd64 dist/agent/agent-linux-arm64 dist/agent/agent-windows-amd64.exe \
      dist/agent/agent-linux-amd64.sha256 dist/agent/agent-linux-arm64.sha256 dist/agent/agent-windows-amd64.exe.sha256
if command -v rustup >/dev/null 2>&1 && rustup target list --installed | grep -qx "$RUST_TARGET"; then
  CC_aarch64_unknown_linux_musl=aarch64-linux-musl-gcc \
  CC_x86_64_unknown_linux_musl=x86_64-linux-musl-gcc \
    cargo build --release --target "$RUST_TARGET" --manifest-path agent-rust/Cargo.toml >/dev/null
  cp "agent-rust/target/$RUST_TARGET/release/api-monitor-agent" "dist/agent/$AGENT_OUT"
else
  log "   跳过 Agent 编译(未安装 $RUST_TARGET);本地镜像将不带 Agent,走远端下载"
fi

log "4/4 打镜像 $IMAGE (基底 $RUNTIME_BASE)"
# .dockerignore 排除了 dist，这里临时放开，构建后还原
restore_ignore() { [ -f .dockerignore.local-backup ] && mv -f .dockerignore.local-backup .dockerignore; }
trap restore_ignore EXIT
cp .dockerignore .dockerignore.local-backup
sed -i '/^dist$/d' .dockerignore

docker build -f Dockerfile.local \
  --build-arg RUNTIME_BASE="$RUNTIME_BASE" \
  --build-arg APP_VERSION="${APP_VERSION:-local-$(date +%Y%m%d%H%M)}" \
  --build-arg AGENT_DOWNLOAD_URL="${AGENT_DOWNLOAD_URL:-https://github.com/iwvw/API-Monitor/releases/download/built-agents}" \
  -t "$IMAGE" . >/dev/null

log "完成：$IMAGE"
docker image inspect "$IMAGE" --format '  size={{.Size}} created={{.Created}}'
