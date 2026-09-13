// refactor-snapshot-diff.mjs
// 对比两份快照，判断重构是否引入非预期行为差异。
//
// 用法：
//   node tools/refactor-snapshot-diff.mjs --before docs/archive/baseline --after docs/archive/after
//
// 退出码：0 = 无非预期差异；1 = 存在差异（重构不合格）。

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

function argValue(name) {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : null;
}

const beforeDir = path.resolve(argValue('--before') || 'docs/archive/baseline');
const afterDir = path.resolve(argValue('--after') || 'docs/archive/after');

function readJson(dir, name) {
  const file = path.join(dir, name);
  if (!fs.existsSync(file)) return null;
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function readText(dir, name) {
  const file = path.join(dir, name);
  if (!fs.existsSync(file)) return null;
  return fs.readFileSync(file, 'utf8');
}

const differences = [];
const allowed = [];

function keyByPath(list, field = 'path') {
  const map = new Map();
  for (const item of list || []) map.set(item[field], item);
  return map;
}

// 1) 路由清单：必须完全一致。
const routesBefore = readText(beforeDir, 'routes.txt');
const routesAfter = readText(afterDir, 'routes.txt');
if (routesBefore !== routesAfter) {
  differences.push('routes.txt 不一致：后端路由契约发生变化');
}

// 2) 接口快照：status / contentType / 归一化 body 必须一致。
const apiBefore = keyByPath(readJson(beforeDir, 'api.json'));
const apiAfter = keyByPath(readJson(afterDir, 'api.json'));
for (const [p, b] of apiBefore) {
  const a = apiAfter.get(p);
  if (!a) {
    differences.push(`api ${p}: 复采缺失`);
    continue;
  }
  if (b.error || a.error) {
    if (String(b.error || '') !== String(a.error || '')) {
      differences.push(`api ${p}: error 变化 before=${b.error} after=${a.error}`);
    }
    continue;
  }
  if (b.status !== a.status) differences.push(`api ${p}: status ${b.status} -> ${a.status}`);
  if (b.contentType !== a.contentType) differences.push(`api ${p}: contentType ${b.contentType} -> ${a.contentType}`);
  if (JSON.stringify(b.body) !== JSON.stringify(a.body)) {
    differences.push(`api ${p}: body 变化\n    before=${JSON.stringify(b.body)}\n    after =${JSON.stringify(a.body)}`);
  }
}

// 3) 静态资源：status / contentType 必须一致。
const staticBefore = keyByPath(readJson(beforeDir, 'static.json'));
const staticAfter = keyByPath(readJson(afterDir, 'static.json'));
for (const [p, b] of staticBefore) {
  const a = staticAfter.get(p);
  if (!a) {
    differences.push(`static ${p}: 复采缺失`);
    continue;
  }
  if (!b.error && !a.error) {
    if (b.status !== a.status) differences.push(`static ${p}: status ${b.status} -> ${a.status}`);
    if (b.contentType !== a.contentType) differences.push(`static ${p}: contentType ${b.contentType} -> ${a.contentType}`);
  }
}

// 4) 构建产物：入口与关键资源必须仍存在；文件数允许变化（拆分改变打包）。
const distBefore = readJson(beforeDir, 'dist.json');
const distAfter = readJson(afterDir, 'dist.json');
if (distBefore && distAfter) {
  if (distBefore.present !== distAfter.present) {
    differences.push(`dist 存在性变化：${distBefore.present} -> ${distAfter.present}`);
  }
  const entryBefore = distBefore.entrypoints || {};
  const entryAfter = distAfter.entrypoints || {};
  for (const name of Object.keys(entryBefore)) {
    if (entryBefore[name] && !entryAfter[name]) {
      differences.push(`构建产物缺少必要入口：${name}`);
    }
  }
  allowed.push(
    `构建产物文件数变化（拆分后允许）：${distBefore.fileCount ?? '?'} -> ${distAfter.fileCount ?? '?'}；` +
      `清单 hash ${distBefore.manifestHash ?? '?'} -> ${distAfter.manifestHash ?? '?'}。入口与关键资源已单独校验。`,
  );
}

console.log('=== 重构一致性对比 ===');
if (allowed.length) {
  console.log('\n允许的差异：');
  for (const line of allowed) console.log('  - ' + line);
}
if (differences.length) {
  console.log('\n非预期差异：');
  for (const line of differences) console.log('  ✗ ' + line);
  console.log(`\n结果：FAIL（${differences.length} 处非预期差异）`);
  process.exit(1);
}
console.log('\n结果：PASS（无非预期差异）');
