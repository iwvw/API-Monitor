// openapi-route-coverage.mjs
// 对比 OpenAPI 文档中的全部 paths 与 Go manifest 路由的覆盖关系：
//   - 覆盖 = path 与某个 manifest 前缀（前缀匹配或 MatchExact 相等）命中
//   - 未覆盖 = OpenAPI 里有、但 AI 通道（manifest.Match）无法识别
// 用法：node tools/openapi-route-coverage.mjs <openapi-json>
// 输出：统计 + 未覆盖清单（含 manifest 归属的模块前缀，若路径整体在 /api 下）
import { readFileSync } from 'node:fs';

const manifestFile = new URL('../backend-go/internal/manifest/manifest.go', import.meta.url);
const manifestSrc = readFileSync(manifestFile, 'utf8');

const routes = [];
const prefixRe = /Prefix:\s*"([^"]+)"/g;
const modeRe = /MatchMode:\s*(MatchExact|MatchPattern)/;
let idx = 0;
for (const m of manifestSrc.matchAll(prefixRe)) {
  const lineStart = manifestSrc.lastIndexOf('\n', m.index) + 1;
  const lineEnd = manifestSrc.indexOf('\n', m.index);
  const line = manifestSrc.slice(lineStart, lineEnd);
  const mode = line.match(modeRe)?.[1] || 'MatchPrefix';
  routes.push({ prefix: m[1], mode });
  idx++;
}

const casc = (path, prefix) => {
  if (path === prefix) return true;
  return path.startsWith(prefix.replace(/\/+$/, '') + '/');
};
const matches = (path, r) =>
  r.mode === 'MatchExact' ? path === r.prefix : casc(path, r.prefix);

const openapiPath = process.argv[2];
if (!openapiPath) {
  console.error('usage: node tools/openapi-route-coverage.mjs <openapi-json>');
  process.exit(2);
}
const doc = JSON.parse(readFileSync(openapiPath, 'utf8'));
const paths = Object.keys(doc.paths || {});
const uncovered = paths.filter((p) => !routes.some((r) => matches(p, r)));

console.log(`manifest routes: ${routes.length}`);
console.log(`openapi paths:   ${paths.length}`);
console.log(`covered:         ${paths.length - uncovered.length}`);
console.log(`uncovered:       ${uncovered.length}`);
if (uncovered.length > 0) {
  console.log('--- uncovered paths ---');
  for (const p of uncovered) console.log(p);
}