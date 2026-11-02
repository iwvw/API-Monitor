#!/usr/bin/env node
/**
 * Go 版本固定点一致性检查。
 *
 * 为什么存在
 * ----------
 * 主构建（go.mod → Dockerfile → ci-cd/release/security/backend-race）都随 go.mod
 * 自动升级，但 `.github/workflows/managed-runtime.yml` 用独立的 `GO_VERSION` 编译
 * sing-box 托管运行时，**不会跟随 go.mod**。CVE-2026-111943（GO-2026-6809）披露时
 * 主构建已是 1.26.10，这个 workflow 还停在 1.26.4，而 govulncheck 只扫 backend-go，
 * 覆盖不到那次独立构建，于是它成了安全基线盲区。
 *
 * 判定规则
 * --------
 *  - 基准：`backend-go/go.mod` 的 `go` 指令。
 *  - 扫描 `.github/workflows/*.yml`（递归）里所有硬编码的 Go 版本：
 *    `GO_VERSION: x.y.z` 与 `go-version: x.y.z`（跳过 `go-version-file`）。
 *  - 低于基准即 ERROR：安全补丁不会被这个 workflow 自动带上。
 *  - 高于基准只提示 warning：那是允许的（独立构建可以先走新工具链）。
 *  - 任何硬编码都提示，鼓励与 go.mod 对齐，但不强制。
 *
 * 用法：node tools/go-version-governance.mjs
 */

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const goModPath = path.join(root, 'backend-go', 'go.mod');
const workflowsDir = path.join(root, '.github', 'workflows');

const errors = [];
const warnings = [];

function readGoModVersion() {
  if (!fs.existsSync(goModPath)) {
    errors.push('找不到 backend-go/go.mod');
    return null;
  }
  const match = fs.readFileSync(goModPath, 'utf8').match(/^\s*go\s+(\d+\.\d+(?:\.\d+)?)\s*$/m);
  if (!match) {
    errors.push('backend-go/go.mod 缺少可解析的 go 指令');
    return null;
  }
  return match[1];
}

function parseVersion(value) {
  return String(value)
    .split('.')
    .map((part) => parseInt(part, 10) || 0);
}

function compareVersions(a, b) {
  const left = parseVersion(a);
  const right = parseVersion(b);
  const length = Math.max(left.length, right.length);
  for (let i = 0; i < length; i += 1) {
    const diff = (left[i] || 0) - (right[i] || 0);
    if (diff !== 0) return diff;
  }
  return 0;
}

function listWorkflowFiles(dir) {
  if (!fs.existsSync(dir)) return [];
  const files = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listWorkflowFiles(full));
    } else if (/\.ya?ml$/.test(entry.name)) {
      files.push(full);
    }
  }
  return files;
}

// 只匹配字面量版本；`${{ env.X }}`、`${{ matrix.x }}` 之类留给人工确认，
// 但同一文件里若定义了 GO_VERSION 常量，常量本身会被下面的规则检查到。
const PIN_PATTERN = /^\s*(GO_VERSION|go-version)\s*:\s*['"]?(\d+\.\d+(?:\.\d+)?)['"]?\s*$/;

const baseline = readGoModVersion();

if (baseline) {
  for (const file of listWorkflowFiles(workflowsDir)) {
    const rel = path.relative(root, file);
    const lines = fs.readFileSync(file, 'utf8').split(/\r?\n/);
    lines.forEach((line, index) => {
      const match = line.match(PIN_PATTERN);
      if (!match) return;
      const [, key, value] = match;
      const where = `${rel}:${index + 1} (${key}: ${value})`;
      const diff = compareVersions(value, baseline);
      if (diff < 0) {
        errors.push(
          `${where} 低于 backend-go/go.mod 的 ${baseline}。` +
          `该构建不会自动跟随 go.mod，Go 安全补丁（如 CVE-2026-111943）会被漏掉。`,
        );
      } else if (diff > 0) {
        warnings.push(`${where} 高于 go.mod 的 ${baseline}（允许，但请确认是有意为之）。`);
      }
    });
  }

  // 顺带校验 Dockerfile 的 go-builder 基础镜像：它同样不会自动跟随 go.mod。
  const dockerfilePath = path.join(root, 'Dockerfile');
  if (fs.existsSync(dockerfilePath)) {
    const dockerfile = fs.readFileSync(dockerfilePath, 'utf8');
    for (const match of dockerfile.matchAll(/^\s*FROM\s+\S*golang:(\d+\.\d+(?:\.\d+)?)/gm)) {
      const value = match[1];
      if (compareVersions(value, baseline) < 0) {
        errors.push(
          `Dockerfile 的 go-builder 镜像 golang:${value} 低于 backend-go/go.mod 的 ${baseline}。`,
        );
      }
    }
  }
}

for (const warning of warnings) {
  console.warn(`warning: ${warning}`);
}

if (errors.length > 0) {
  console.error('Go version governance check failed:');
  for (const error of errors) {
    console.error(`  - ${error}`);
  }
  process.exit(1);
}

console.log(
  `Go version governance check passed (baseline backend-go/go.mod ${baseline}; ` +
  `${listWorkflowFiles(workflowsDir).length} workflow files scanned).`,
);
