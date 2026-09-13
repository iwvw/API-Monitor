// refactor-snapshot.mjs
// 重构行为一致性验证的快照采集工具。
//
// 用法：
//   node tools/refactor-snapshot.mjs --out docs/archive/baseline
//   API_MONITOR_BASE_URL=http://127.0.0.1:3100 node tools/refactor-snapshot.mjs --out /tmp/after
//
// 采集内容：
//   1. routes.txt      —— manifest 路由清单（后端对外契约）
//   2. api.json        —— 一组只读接口的「状态码 + 归一化响应体」
//   3. static.json     —— 关键静态资源的状态码与 Content-Type
//   4. dist.json       —— dist/ 构建产物的文件清单与内容 hash
//
// 归一化：剔除 requestId / Date / X-Request-Id 等易变字段，保证基线可比。

import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import http from 'node:http';
import process from 'node:process';

const baseUrl = (process.env.API_MONITOR_BASE_URL || 'http://127.0.0.1:3000').replace(/\/+$/, '');

const outArgIndex = process.argv.indexOf('--out');
const outDir = path.resolve(outArgIndex >= 0 ? process.argv[outArgIndex + 1] : 'docs/archive/baseline');

// 只读接口探测集：覆盖未鉴权边界、公开接口、元信息接口。
// 方法固定 GET，不产生任何写副作用。
const API_PROBES = [
  '/health',
  '/api/auth/session',
  '/api/auth/login-options',
  '/api/auth/check-password',
  '/api/ai/manifest',
  '/api/openapi.json',
  '/api/settings',
  '/api/uptime/public/probe',
  '/api/server/public/probe',
  '/api/github/public/probe',
  '/api/backup',
  '/api/notification',
  '/api/openai',
  '/api/uptime',
  '/api/nonexistent-refactor-probe',
];

// 关键静态资源：状态码与 Content-Type 必须与基线一致。
const STATIC_PROBES = [
  '/',
  '/robots.txt',
  '/manifest.webmanifest',
  '/logo.svg',
  '/llms.txt',
  '/assets-probe-missing.js',
];

// 响应体中需要归一化（视为易变）的字段名。
const VOLATILE_FIELDS = ['requestId', 'lockUntil', 'date', 'expiresAt', 'lastAttempt', 'createdAt', 'updatedAt', 'uptime'];

function classifyContentType(value) {
  if (!value) return '';
  return value.split(';')[0].trim().toLowerCase();
}

function normalizeValue(value) {
  if (Array.isArray(value)) return value.map(normalizeValue);
  if (value && typeof value === 'object') {
    const out = {};
    for (const key of Object.keys(value).sort()) {
      if (VOLATILE_FIELDS.includes(key)) continue;
      out[key] = normalizeValue(value[key]);
    }
    return out;
  }
  return value;
}

function normalizeBody(text) {
  const trimmed = (text || '').trim();
  if (!trimmed) return { kind: 'empty' };
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    // HTML / 纯文本：记录长度与是否含 SPA 根标记，避免整页体积进快照。
    return {
      kind: 'text',
      length: trimmed.length,
      hasAppRoot: trimmed.includes('API Monitor') || trimmed.includes('<div id="root">'),
    };
  }
  try {
    return { kind: 'json', value: normalizeValue(JSON.parse(trimmed)) };
  } catch {
    return { kind: 'text', length: trimmed.length, hasAppRoot: false };
  }
}

function fetchOnce(url) {
  return new Promise((resolve, reject) => {
    const req = http.get(url, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => {
        resolve({
          status: res.statusCode,
          contentType: classifyContentType(res.headers['content-type']),
          body: Buffer.concat(chunks).toString('utf8'),
        });
      });
    });
    req.on('error', reject);
    req.setTimeout(8000, () => req.destroy(new Error(`timeout: ${url}`)));
  });
}

function fileHash(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex').slice(0, 16);
}

function collectDist() {
  const distDir = path.resolve('dist');
  if (!fs.existsSync(distDir)) return { present: false, files: [] };
  const files = [];
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
      } else {
        files.push({
          path: path.relative(distDir, full).split(path.sep).join('/'),
          size: fs.statSync(full).size,
          hash: fileHash(full),
        });
      }
    }
  };
  walk(distDir);
  return { present: true, files };
}

function collectRoutes() {
  const manifestPath = 'backend-go/internal/manifest/manifest.go';
  if (!fs.existsSync(manifestPath)) return [];
  const manifest = fs.readFileSync(manifestPath, 'utf8');
  return [...manifest.matchAll(/\{Prefix:\s*"([^"]+)",\s*Module:\s*"([^"]+)",\s*Owner:\s*(Owner\w+)/g)]
    .map(([, prefix, module, owner]) => `${owner.replace(/^Owner/, '').toLowerCase()} ${prefix} ${module}`)
    .sort();
}

async function collectApi() {
  const out = [];
  for (const p of API_PROBES) {
    try {
      const res = await fetchOnce(`${baseUrl}${p}`);
      out.push({ path: p, status: res.status, contentType: res.contentType, body: normalizeBody(res.body) });
    } catch (err) {
      out.push({ path: p, error: String(err.message || err) });
    }
  }
  return out;
}

async function collectStatic() {
  const out = [];
  for (const p of STATIC_PROBES) {
    try {
      const res = await fetchOnce(`${baseUrl}${p}`);
      out.push({ path: p, status: res.status, contentType: res.contentType });
    } catch (err) {
      out.push({ path: p, error: String(err.message || err) });
    }
  }
  return out;
}

async function main() {
  fs.mkdirSync(outDir, { recursive: true });

  const routes = collectRoutes();
  fs.writeFileSync(path.join(outDir, 'routes.txt'), routes.join('\n') + (routes.length ? '\n' : ''));

  const dist = collectDist();
  fs.writeFileSync(path.join(outDir, 'dist.json'), JSON.stringify(dist, null, 2) + '\n');

  const api = await collectApi();
  fs.writeFileSync(path.join(outDir, 'api.json'), JSON.stringify(api, null, 2) + '\n');

  const staticAssets = await collectStatic();
  fs.writeFileSync(path.join(outDir, 'static.json'), JSON.stringify(staticAssets, null, 2) + '\n');

  console.log(`snapshot written to ${outDir}`);
  console.log(`  routes: ${routes.length}`);
  console.log(`  api probes: ${api.length}`);
  console.log(`  static probes: ${staticAssets.length}`);
  console.log(`  dist files: ${dist.files.length}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
