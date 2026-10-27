#!/usr/bin/env node
/**
 * 托管运行时目录（catalog）与发布 workflow 的一致性检查。
 *
 * 为什么存在
 * ----------
 * `.github/workflows/managed-runtime.yml` 编译 sing-box 并校验两个 SHA256，
 * `backend-go/internal/serveragent/proxy_runtime_catalog.go` 里也硬编码了同一组
 * URL 与 SHA256（Agent 下载后据此校验）。这两处必须严格一致，否则：
 *   - 只改 workflow → 产物发布成功，但面板下发的是旧摘要 → 所有节点校验失败；
 *   - 只改 catalog → 面板指向不存在的 release → 下载 404。
 * 这类错配只在真实发布并部署到主机后才暴露，代价很高，所以在 CI 里静态比对。
 *
 * 检查项
 * ------
 *  1. catalog 的 AMD64SHA256 / ARM64SHA256 与 workflow `sha256sum --check` 的期望值一致。
 *  2. catalog 的 release tag（URL 里的 /releases/download/<tag>/）与 workflow 的
 *     `tag_name` 一致。
 *  3. catalog 的 Version 前缀与 workflow 的 SING_BOX_VERSION 一致
 *     （允许 -amN 之类的本仓库构建序号后缀）。
 *  4. workflow 的 concurrency group 与 tag 对应（防止两个构建互相取消）。
 *
 * 用法：node tools/runtime-catalog-governance.mjs
 */

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const catalogPath = path.join(root, 'backend-go', 'internal', 'serveragent', 'proxy_runtime_catalog.go');
const workflowPath = path.join(root, '.github', 'workflows', 'managed-runtime.yml');

const errors = [];

for (const [label, file] of [['catalog', catalogPath], ['workflow', workflowPath]]) {
  if (!fs.existsSync(file)) {
    console.error(`runtime catalog governance: 找不到 ${label} 文件 ${path.relative(root, file)}`);
    process.exit(1);
  }
}

const catalog = fs.readFileSync(catalogPath, 'utf8');
const workflow = fs.readFileSync(workflowPath, 'utf8');

function catalogField(name) {
  const match = catalog.match(new RegExp(`${name}:\\s*"([^"]*)"`));
  return match ? match[1] : null;
}

function workflowField(pattern, label) {
  const match = workflow.match(pattern);
  if (!match) {
    errors.push(`workflow 缺少 ${label}`);
    return null;
  }
  return match[1];
}

// 把 workflow 里的 `${{ env.X }}` 按同文件 env 段展开，才能比对 tag_name。
function expandEnvExpressions(value) {
  if (!value) return value;
  return value.replace(/\$\{\{\s*env\.([A-Z0-9_]+)\s*\}\}/g, (whole, name) => {
    const match = workflow.match(new RegExp(`^\\s*${name}:\\s*(\\S+)\\s*$`, 'm'));
    return match ? match[1] : whole;
  });
}

// --- workflow 侧 -----------------------------------------------------------
const workflowTagRaw = workflowField(/^\s*tag_name:\s*(.+?)\s*$/m, 'tag_name');
const workflowTag = expandEnvExpressions(workflowTagRaw);
const workflowVersion = workflowField(/^\s*SING_BOX_VERSION:\s*(\S+)\s*$/m, 'SING_BOX_VERSION');
const concurrencyGroup = workflowField(/^\s*group:\s*(\S+)\s*$/m, 'concurrency group');

const workflowDigests = new Map();
for (const match of workflow.matchAll(
  /^\s*echo '([0-9a-f]{64})\s+sing-box-[0-9.]*-linux-(amd64|arm64)'\s*\|\s*sha256sum --check/gm,
)) {
  workflowDigests.set(match[2], match[1]);
}

if (workflowDigests.size !== 2) {
  errors.push(
    `workflow 里应有 amd64 与 arm64 两条 sha256sum --check 期望值，实际找到 ${workflowDigests.size} 条`,
  );
}

// --- catalog 侧 ------------------------------------------------------------
const catalogVersion = catalogField('Version');
const amd64URL = catalogField('AMD64URL');
const arm64URL = catalogField('ARM64URL');
const amd64SHA = catalogField('AMD64SHA256');
const arm64SHA = catalogField('ARM64SHA256');

// 1. 摘要一致
if (workflowDigests.size === 2) {
  if (amd64SHA !== workflowDigests.get('amd64')) {
    errors.push(
      `AMD64SHA256 不一致：catalog=${amd64SHA} workflow=${workflowDigests.get('amd64')}`,
    );
  }
  if (arm64SHA !== workflowDigests.get('arm64')) {
    errors.push(
      `ARM64SHA256 不一致：catalog=${arm64SHA} workflow=${workflowDigests.get('arm64')}`,
    );
  }
}

// 2. release tag 一致
if (workflowTag && amd64URL && arm64URL) {
  const tagPattern = /\/releases\/download\/([^/]+)\//;
  for (const [label, url] of [['AMD64URL', amd64URL], ['ARM64URL', arm64URL]]) {
    const match = url.match(tagPattern);
    if (!match) {
      errors.push(`catalog 的 ${label} 不是 GitHub release 下载地址：${url}`);
      continue;
    }
    if (match[1] !== workflowTag) {
      errors.push(`catalog 的 ${label} 指向 release "${match[1]}"，workflow 发布的是 "${workflowTag}"`);
    }
  }
}

// 3. 版本前缀一致（允许 -amN 之类的构建序号后缀）
if (workflowVersion && catalogVersion) {
  if (!catalogVersion.startsWith(workflowVersion)) {
    errors.push(
      `catalog Version "${catalogVersion}" 不以 workflow SING_BOX_VERSION "${workflowVersion}" 开头`,
    );
  }
}

// 4. concurrency 与 tag 对应
if (concurrencyGroup && workflowTag) {
  if (!concurrencyGroup.includes(workflowTag)) {
    errors.push(
      `workflow concurrency group "${concurrencyGroup}" 未包含 release tag "${workflowTag}"，` +
      `不同 release 的构建会互相取消`,
    );
  }
}

if (errors.length > 0) {
  console.error('Runtime catalog governance check failed:');
  for (const error of errors) {
    console.error(`  - ${error}`);
  }
  process.exit(1);
}

console.log(
  `Runtime catalog governance check passed (release ${workflowTag}, ` +
  `sing-box ${catalogVersion}, amd64/arm64 digests pinned).`,
);
