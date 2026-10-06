import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { execFileSync } from 'node:child_process';

// Agent 版本治理：凡改动 agent-rust 二进制输入（src/、vendor/、Cargo.toml、
// Cargo.lock），必须同时递增 [package].version。
//
// 背景：滚动 release（built-agents）按版本号识别新旧；若改了 Agent 源码却没
// bump 版本，三平台二进制会被重建但版本号不变，各主机 batch-upgrade 无从判断
// 是否需要更新，导致「代码已发布、主机永远升不上去」。
//
// 分层：
//   - ERROR：agent 二进制输入已变更，但版本未严格递增（含 Cargo.toml 与
//     Cargo.lock 版本不一致）。
//   - 无 base ref（浅克隆等）时跳过 diff 校验，仅保留版本一致性静态校验；
//     dev 推送的权威门禁在 ci-cd.yml 的 detect-agent-changes job（fetch-depth: 0）。

const root = process.cwd();
const errors = [];
const notes = [];

const tomlRel = 'agent-rust/Cargo.toml';
const lockRel = 'agent-rust/Cargo.lock';
const binaryPrefixes = ['agent-rust/src/', 'agent-rust/vendor/'];
const binaryExact = new Set([tomlRel, lockRel]);

function read(rel) {
  return fs.readFileSync(path.join(root, rel), 'utf8');
}

function parseTomlVersion(toml) {
  const pkg = toml.match(/\[package\][\s\S]*?(?=\n\[|$)/);
  const scope = pkg ? pkg[0] : toml;
  const match = scope.match(/^\s*version\s*=\s*"([^"]+)"/m);
  return match ? match[1] : null;
}

function parseLockVersion(lock) {
  const match = lock.match(/\[\[package\]\]\nname = "api-monitor-agent"\nversion = "([^"]+)"/);
  return match ? match[1] : null;
}

function semver(value) {
  return String(value)
    .split('.')
    .map((part) => parseInt(part, 10) || 0);
}

function isGreater(a, b) {
  const x = semver(a);
  const y = semver(b);
  for (let i = 0; i < 3; i += 1) {
    if (x[i] !== y[i]) return x[i] > y[i];
  }
  return false;
}

function git(args) {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
}

function gitOk(args) {
  try {
    git(args);
    return true;
  } catch {
    return false;
  }
}

function resolveBase(explicit) {
  const candidates = [];
  if (explicit) candidates.push(explicit);
  if (process.env.AGENT_VERSION_BASE) candidates.push(process.env.AGENT_VERSION_BASE);
  if (process.env.GITHUB_EVENT_BEFORE) candidates.push(process.env.GITHUB_EVENT_BEFORE);
  if (process.env.GITHUB_BASE_REF) candidates.push(process.env.GITHUB_BASE_REF);
  if (process.env.BASE_SHA) candidates.push(process.env.BASE_SHA);
  for (const candidate of candidates) {
    const ref = String(candidate || '').trim();
    if (!ref || /^0+$/.test(ref)) continue;
    if (gitOk(['rev-parse', '--verify', '--quiet', `${ref}^{commit}`])) return ref;
  }
  for (const ref of ['origin/dev', 'origin/main', 'dev', 'main', 'HEAD~1']) {
    if (gitOk(['rev-parse', '--verify', '--quiet', `${ref}^{commit}`])) return ref;
  }
  return null;
}

const cliArgs = process.argv.slice(2);
let baseArg = null;
for (let i = 0; i < cliArgs.length; i += 1) {
  if (cliArgs[i] === '--base') baseArg = cliArgs[i + 1];
}

const toml = read(tomlRel);
const lock = read(lockRel);
const headVersion = parseTomlVersion(toml);
const lockVersion = parseLockVersion(lock);

if (!headVersion) {
  errors.push(`cannot parse [package].version from ${tomlRel}`);
}
if (headVersion && lockVersion && headVersion !== lockVersion) {
  errors.push(
    `agent version mismatch: ${tomlRel}=${headVersion} vs ${lockRel}=${lockVersion} (run cargo check to sync Cargo.lock)`,
  );
}

const base = resolveBase(baseArg);
if (!base) {
  console.log('Agent version governance: base ref unavailable, skipped change-diff check (static consistency still enforced).');
} else {
  const changedFiles = (() => {
    try {
      return git(['diff', '--name-only', `${base}...HEAD`])
        .split('\n')
        .map((line) => line.trim())
        .filter(Boolean);
    } catch {
      return [];
    }
  })();

  const baseVersion = (() => {
    try {
      return parseTomlVersion(git(['show', `${base}:${tomlRel}`]));
    } catch {
      return null;
    }
  })();

  const binaryChanged = changedFiles.filter(
    (file) => binaryExact.has(file) || binaryPrefixes.some((prefix) => file.startsWith(prefix)),
  );

  if (baseVersion && headVersion && binaryChanged.length > 0 && !isGreater(headVersion, baseVersion)) {
    const sample = binaryChanged.slice(0, 6).join(', ');
    const more = binaryChanged.length > 6 ? ` (+${binaryChanged.length - 6} more)` : '';
    errors.push(
      `agent binary inputs changed but version not bumped (${baseVersion} -> ${headVersion}). ` +
        `Bump [package].version in ${tomlRel} (and ${lockRel}). Changed: ${sample}${more}`,
    );
  }

  notes.push(`base=${base} baseVersion=${baseVersion || '?'} headVersion=${headVersion || '?'} binaryFilesChanged=${binaryChanged.length}`);
}

for (const note of notes) console.log(`Agent version governance: ${note}`);

if (errors.length) {
  console.error('Agent version governance check failed:');
  for (const error of errors) console.error(`  - ${error}`);
  process.exit(1);
}

console.log(`Agent version governance check passed (api-monitor-agent ${headVersion}).`);
