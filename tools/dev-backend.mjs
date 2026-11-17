#!/usr/bin/env node
// 跨平台启动 Go 开发后端。
//
// 背景:package.json 的 backend-go:dev 原先是一行 PowerShell(读根 .env 注入
// ENCRYPTION_KEY 后再 go run),在 Linux/macOS 上直接失败(powershell 不存在)。
// 这里用 Node 做同样的注入,让 Windows / macOS / Linux 共用一条命令。
//
// 环境变量装载规则:
//   .env → .env.local(后者覆盖前者),已存在的进程环境变量不会被覆盖。
// 与 Vite 的 loadEnv 读取的 .env.local、以及 Go 侧
// backend-go/internal/config/dotenv.go 的「进程环境优先」语义保持一致。
import { spawn } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const backendDir = path.join(repoRoot, 'backend-go');
const ENV_FILES = ['.env', '.env.local'];

/**
 * 解析简单 KEY=VALUE 格式(与 backend-go/internal/config/dotenv.go 的
 * readDotEnvFile 兼容):忽略空行/注释行,剥离 UTF-8 BOM,键值两侧去空白,
 * 值首尾引号被去除。
 */
export function parseDotEnv(text) {
  const values = new Map();
  const lines = String(text)
    .replace(/^\uFEFF/, '')
    .split(/\r?\n/);
  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#') || !line.includes('=')) continue;
    const index = line.indexOf('=');
    const key = line.slice(0, index).trim();
    if (!key) continue;
    const value = line
      .slice(index + 1)
      .trim()
      .replace(/^["']+/, '')
      .replace(/["']+$/, '');
    values.set(key, value);
  }
  return values;
}

function loadEnvironment() {
  const injected = [];
  for (const file of ENV_FILES) {
    const fullPath = path.join(repoRoot, file);
    if (!existsSync(fullPath)) continue;
    for (const [key, value] of parseDotEnv(readFileSync(fullPath, 'utf8'))) {
      if (process.env[key] !== undefined) continue;
      process.env[key] = value;
      injected.push(`${key}(${file})`);
    }
  }
  return injected;
}

const injected = loadEnvironment();
if (injected.length > 0) {
  console.log(`[backend-go:dev] 已注入环境变量:${injected.join(', ')}`);
}

const child = spawn('go', ['run', './cmd/api-monitor'], {
  cwd: backendDir,
  env: process.env,
  stdio: 'inherit',
});

child.on('error', (error) => {
  console.error(`[backend-go:dev] 启动 go run 失败:${error.message}`);
  process.exit(1);
});

// 把终端/父进程的退出信号转给 go run,避免留下孤儿后端进程
// (Windows 上 npm run dev 被 Ctrl+C 中断时同样生效)。
// SIGHUP 在 Windows 不可用,按平台裁剪,避免 kill 抛错。
const FORWARD_SIGNALS = process.platform === 'win32'
  ? ['SIGINT', 'SIGTERM']
  : ['SIGINT', 'SIGTERM', 'SIGHUP'];
let interrupted = false;
for (const signal of FORWARD_SIGNALS) {
  process.on(signal, () => {
    interrupted = true;
    if (!child.killed) child.kill(signal);
  });
}

child.on('exit', (code, signal) => {
  // 用户主动 Ctrl+C 时安静退出,不把中断当成启动失败刷日志。
  if (interrupted) process.exit(0);
  if (signal) {
    console.error(`[backend-go:dev] go run 被信号 ${signal} 终止`);
    process.exit(1);
  }
  process.exit(code ?? 1);
});