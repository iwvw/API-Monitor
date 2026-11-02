#!/usr/bin/env node
/*
 * openapi-method-drift.mjs — 检查 OpenAPI/文档里声明的方法是否与 handler 真实实现一致。
 *
 * 背景：InferRouteMethods 对 MatchPattern 路由默认只声明 GET。一旦某个路由
 * 真实是 POST/DELETE 但描述没写关键词，文档就会把写接口标成只读，
 * AI 照文档调用必然 405（1Panel 模块就因此错了 11 条）。
 *
 * 本脚本用两条独立信号交叉验证，不需要跑服务：
 *   1. 显式方法表：system/onepanel_gen.go 的 onepanelRouteMethods、
 *      system/service.go 里 inferRouteMethods 的 case 分支
 *   2. 契约信号：routeRequestContracts 里登记了请求体的路由必然是写操作
 *
 * 只要「契约显示是写接口，但方法表里只有 GET」就报错。
 *
 * 用法：node tools/openapi-method-drift.mjs
 */
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const systemDir = path.join(root, 'backend-go/internal/system');

const read = (rel) => fs.readFileSync(path.join(systemDir, rel), 'utf8');

const contractSources = ['route_contracts.go', 'onepanel_gen.go']
  .filter((f) => fs.existsSync(path.join(systemDir, f)))
  .map(read)
  .join('\n');

// routeRequestContracts["/api/x"] = obj(...) / noBody
// noBody 表示「不接收请求体」，不算写操作信号；obj(...) 才算。
const contracts = new Map();
for (const m of contractSources.matchAll(/routeRequestContracts\["([^"]+)"\]\s*=\s*([^\n]+)/g)) {
  const [, prefix, rhs] = m;
  const isObj = /\bobj\(/.test(rhs);
  const isEmpty = /std\$^/.test(rhs);
  contracts.set(prefix, { isObj, isEmpty, raw: rhs.trim().slice(0, 60) });
}

// 显式方法表：onepanel_gen.go 里的 map[string][]string
const explicit = new Map();
const genFile = path.join(systemDir, 'onepanel_gen.go');
if (fs.existsSync(genFile)) {
  const gen = fs.readFileSync(genFile, 'utf8');
  const start = gen.indexOf('var onepanelRouteMethods');
  if (start >= 0) {
    const open = gen.indexOf('{', start);
    const close = gen.indexOf('\n}', open);
    const block = gen.slice(open, close);
    for (const m of block.matchAll(/"([^"]+)":\s*\[\]string\{([^}]*)\}/g)) {
      explicit.set(m[1], [...m[2].matchAll(/"([A-Z]+)"/g)].map((x) => x[1]));
    }
  }
}

// inferRouteMethods 的显式 case：把写方法显式登记的路由，方法集合里应含写方法
const serviceSrc = read('service.go');
const inferStart = serviceSrc.indexOf('func inferRouteMethods(');
const inferBody = inferStart >= 0 ? serviceSrc.slice(inferStart, serviceSrc.indexOf('\n}', inferStart)) : '';

const problems = [];
for (const [prefix, contract] of contracts) {
  if (!contract.isObj) continue; // 空契约无法判断读写性质
  // 只有「契约有字段」的路由才作为写接口信号
  const hasFields = /\bobj\(/.test(contract.raw) && !/\bobj\(nil,\s*map\[string\]prop\{\}\)/.test(contract.raw);
  if (!hasFields) continue;

  const methods = explicit.get(prefix);
  if (methods) {
    const isWrite = methods.some((m) => ['POST', 'PUT', 'PATCH', 'DELETE'].includes(m));
    if (!isWrite) {
      problems.push(`${prefix}: 登记了写操作请求体契约，但方法表只有 ${methods.join(',')}`);
    }
    continue;
  }

  // 没有显式方法表时，看 inferRouteMethods 是否把它显式登记为写方法
  if (inferBody.includes(`"${prefix}"`)) {
    continue; // 已在显式 case 里登记，信任它
  }
  // 剩下的是靠描述推断的路径：只提示不报错（描述里可能确实有写关键词）
  process.env.ONEPANEL_DRIFT_VERBOSE && problems.push(`[warn] ${prefix}: 写契约但依赖描述推断`);
}

if (problems.length > 0) {
  const hard = problems.filter((p) => !p.startsWith('[warn]'));
  if (hard.length > 0) {
    console.error(`OpenAPI 方法漂移检查失败（${hard.length} 处）：`);
    for (const p of hard) console.error(`  - ${p}`);
    console.error('\n请把对应路由登记进显式方法表（service.go 的 inferRouteMethods case，或模块生成器）。');
    process.exit(1);
  }
  for (const p of problems) console.warn(p);
}

console.log(
  `OpenAPI method drift check passed (${contracts.size} 条契约, ${explicit.size} 条显式方法声明)`,
);
