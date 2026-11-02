#!/usr/bin/env node
/*
 * gen-api-catalog.mjs — 生成模块「API 接口目录全面接入」的 Go 骨架。
 *
 * 用法：
 *   node tools/gen-api-catalog.mjs <module>          # 打印到 stdout
 *   node tools/gen-api-catalog.mjs <module> --apply  # 追加写入对应文件
 *
 * 覆盖 4 处登记：
 *   1. manifest.go 子路由（若该模块只有前缀，会提示先补 MatchPattern）
 *   2. route_descriptions.go 中文描述骨架
 *   3. api_docs_catalog.go apiDocSeeds 骨架（写接口方法标 TODO，需人工核对）
 *   4. route_contracts.go 写接口契约骨架（默认 noBody，需人工补请求体字段）
 * 另外输出分组接线提示（system/service.go routeGroup + ApiDocsPage 四处）。
 *
 * 生成骨架后必须人工过一遍：确认 Methods、补充中文描述与请求体字段，
 * 再跑 go test -count=1 ./internal/system/...（契约覆盖强制审计）。
 */
import fs from 'node:fs';
import path from 'node:path';

const repoRoot = path.resolve(import.meta.dirname, '..');
const manifestPath = path.join(repoRoot, 'backend-go/internal/manifest/manifest.go');
const systemDir = path.join(repoRoot, 'backend-go/internal/system');

const module = process.argv[2];
const apply = process.argv.includes('--apply');
if (!module) {
  console.error('用法: node tools/gen-api-catalog.mjs <module> [--apply]');
  process.exit(1);
}

const src = fs.readFileSync(manifestPath, 'utf8');
const routes = [];
for (const m of src.matchAll(/\{[^}]*\}/g)) {
  const block = m[0];
  const pm = block.match(/Prefix:\s*"([^"]+)"/);
  const mm = block.match(/Module:\s*"([^"]+)"/);
  if (!pm || !mm || mm[1] !== module) continue;
  const matchMode = block.match(/MatchMode:\s*(\w+)/);
  routes.push({
    prefix: pm[1],
    matchMode: matchMode ? matchMode[1] : '(prefix)',
  });
}
routes.sort((a, b) => a.prefix.localeCompare(b.prefix));

if (routes.length === 0) {
  console.error(`manifest.go 中未找到 Module="${module}" 的路由`);
  process.exit(1);
}

const isWrite = (p) => /(actions|reset-password|associate|disassociate|import|verify|defaults|default-project|rename|labels|resize|snapshot|buckets\/\{[^}]+\}|recordsets\/\{[^}]+\})/.test(p);

function guessMethods(r) {
  const p = r.prefix;
  const tail = p.split('/').pop();
  if (['verify', 'import', 'actions', 'reset-password', 'associate', 'disassociate', 'resize', 'snapshot', 'labels', 'objects'].includes(tail)) return ['GET'];
  if (tail === 'accounts' && r.matchMode === 'MatchExact') return ['GET'];
  if (p.includes('/recordsets/{recordsetId}')) return ['PUT'];
  if (p.includes('/buckets/{bucket}')) return ['DELETE'];
  if (p.includes('/buckets')) return ['GET'];
  if (p.includes('/instances/{') && /actions|reset-password/.test(p)) return ['POST'];
  return ['GET'];
}
const isWriteRoute = (r) => /(actions|reset-password|associate|disassociate|import|verify|defaults|default-project|rename|labels|resize|snapshot)/.test(r.prefix);

const groupDisplay = module === 'huawei' ? '华为云' : module === 'gcp' ? 'Google Cloud' : module.toUpperCase();

let out = '';
out += `\n// ==== 生成骨架：Module="${module}"（${routes.length} 条） ====\n`;

// 1. manifest 提示
out += `\n// [1] manifest.go 子路由（若该模块当前只有前缀，需在此逐条补 MatchExact/MatchPattern）\n`;
out += routes.filter((r) => r.matchMode === '(prefix)' && r.prefix !== `/api/${module}`).map((r) => {
  return `//   补: {Prefix: "${r.prefix}", Module: "${module}", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, ... MatchMode: MatchPattern}`;
}).join('\n') || '//   已有子路由登记，无需补\n';

// 2. route_descriptions.go
out += `\n// [2] route_descriptions.go 中文描述骨架（粘贴到 routeDescriptions 表，人工改描述）\n`;
out += routes.map((r) => `	"${r.prefix}": "${module} ${r.prefix.replace(/^\/api\/[^/]*/, '').replace(/\//g, ' ').trim()}",`).join('\n');

// 3. api_docs_catalog.go
out += `\n// [3] api_docs_catalog.go apiDocSeeds 骨架（写接口 Methods 必须人工核对，避免 infer 默认 GET）\n`;
out += routes.filter((r) => r.matchMode !== '(prefix)' || r.prefix === `/api/${module}`).map((r) => {
  const methods = guessMethods(r);
  const todo = isWriteRoute(r) ? ' // TODO: 核对 Methods；POST/PUT 需显式' : '';
  return `	{Route: manifest.Route{Prefix: "${r.prefix}", Module: "${module}", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "${module} route"${r.matchMode !== '(prefix)' ? `, MatchMode: manifest.${r.matchMode === 'MatchExact' ? 'MatchExact' : 'MatchPattern'}` : ''}},
		Docs: apiRouteDocs{Methods: []string{"${methods.join('", "')}"}}},${todo}`;
}).join('\n');

// 4. route_contracts.go
out += `\n// [4] route_contracts.go 写接口契约骨架（写接口默认 noBody，需人工补请求体字段）\n`;
out += routes.filter(isWriteRoute).map((r) => `	routeRequestContracts["${r.prefix}"] = noBody // TODO: 补请求体 schema（obj/…）`).join('\n');

// 5. 分组提示
out += `\n// [5] 分组接线提示：\n//   后端 system/service.go routeGroup(): case strings.HasPrefix(prefix, "/api/${module}"): return "${groupDisplay}"\n`;
out += `//   前端 ApiDocsPage.jsx: routeGroup() 加分支；GROUP_ORDER 加 "${groupDisplay}"；GROUP_NAME_TO_MODULE_ID 加 "${groupDisplay}": "${module}"；MODULE_LABELS 加 ${module}: "${groupDisplay}"\n`;

console.log(out);

if (apply) {
  const pathMap = {
    'route_descriptions.go': '',
    'api_docs_catalog.go': '',
    'route_contracts.go': '',
    'manifest.go': '',
  };
  // 只追加到 descriptions / catalog / contracts 三处（manifest 涉及匹配策略，人工核对后手补）
  const sections = [];
  for (const [fname] of Object.entries(pathMap)) {
    if (fname === 'manifest.go') continue;
    sections.push({ fname });
  }
  const sb = [`// ===== Module="${module}" 生成骨架 =====`];
  console.error('--apply 仅打印分段，手动粘贴更安全（避免破坏既有登记顺序）。请勿自动写入大文件。');
  console.log(sb.join('\n'));
}