#!/usr/bin/env node
/**
 * Iconify 图标集按需裁剪：把「整包 7.9MB」降到「实际用到的几个」。
 *
 * 为什么存在
 * ----------
 * 公开书签页曾直接 `import logosIcons from '@iconify-json/logos/icons.json'`
 * （`src/js/pages/bookmarks/PublicHero.jsx`）。该 JSON 有 2282 个图标、7.9MB，
 * 构建后公开页 chunk 达 **8.0MB**（gzip 2.75MB / brotli 2.36MB），
 * 而全项目只用得到 3 个。首屏体积的 99% 是没人看的图标。
 *
 * 做法
 * ----
 *  - 扫描 `src/js/**` 里的 `集合:图标名` 字面量，只认**已安装**的
 *    `@iconify-json/<集合>` 包（避免把 `http://`、`React:Hooks` 之类误判成图标）；
 *  - 从对应 icons.json 抽出这些条目（含 alias → parent 解析），
 *    写成 `src/assets/iconify/<集合>.subset.json`；
 *  - 同时生成 `src/assets/iconify/index.js`，导出集合数组与 `hasIcon()`，
 *    让 UI 在渲染前就能判断「这个图标在不在打包结果里」，不必等 Iconify
 *    去请求 api.iconify.design 再失败（内网/离线时那是静默空白）。
 *
 * 约定
 * ----
 *  - 扫描不到任何图标时不生成空集合，而是保留已有产物（避免误删）。
 *  - 用户自定义搜索引擎可以填任意 `logos:xxx`；未命中的名字由
 *    `EngineIconPreview` 回退成「品牌色圆底 + 首字」，这是既有的兜底路径。
 *  - 生成的 JSON/JS 已入库，`npm run build` 前会重跑，保证与源码一致。
 *
 * 用法：
 *   node tools/generate-iconify-subset.mjs           # 生成
 *   node tools/generate-iconify-subset.mjs --check   # 只校验产物是否最新（CI 用）
 */

import { existsSync, mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const srcDir = path.join(rootDir, 'src', 'js');
const outDir = path.join(rootDir, 'src', 'assets', 'iconify');
const checkMode = process.argv.includes('--check');
const sourceExtensions = new Set(['.js', '.jsx']);

/** 已安装的 @iconify-json 包 → 集合前缀。只扫这些，避免把普通冒号文本当图标。 */
function installedCollections() {
  const scopeDir = path.join(rootDir, 'node_modules', '@iconify-json');
  if (!existsSync(scopeDir)) return new Map();
  const map = new Map();
  for (const entry of readdirSync(scopeDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const iconsPath = path.join(scopeDir, entry.name, 'icons.json');
    if (existsSync(iconsPath)) map.set(entry.name, iconsPath);
  }
  return map;
}

function collectSourceFiles(dir) {
  const result = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      result.push(...collectSourceFiles(fullPath));
      continue;
    }
    if (sourceExtensions.has(path.extname(entry.name))) result.push(fullPath);
  }
  return result;
}

/**
 * 去掉注释后再扫描：注释里常出现 `logos:xxx` 这类示例/占位符，
 * 把它们当成"用到了"会产出空壳条目并干扰缺失告警。
 *
 * 只用于扫描，不影响产物。字符串里的 `//`（如 https://）会被误当行注释起点，
 * 但那只影响这一行的后续匹配 —— 而图标名本身在 `//` 之前就已匹配完，
 * 所以对结果无影响。
 */
function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

/** 解析 `集合:图标名`。前缀必须是已安装集合，否则跳过。 */
function collectUsedIcons(prefixes) {
  const used = new Map(); // prefix -> Set(name)
  for (const file of collectSourceFiles(srcDir)) {
    const source = stripComments(readFileSync(file, 'utf8'));
    for (const match of source.matchAll(/\b([a-z0-9-]+):([a-z0-9][a-z0-9-]*)\b/g)) {
      const [, prefix, name] = match;
      if (!prefixes.has(prefix)) continue;
      if (!used.has(prefix)) used.set(prefix, new Set());
      used.get(prefix).add(name);
    }
  }
  return used;
}

/** alias 需要带上 parent，否则 Iconify 解不出来。 */
function resolveEntry(collection, name) {
  if (collection.icons?.[name]) return { kind: 'icon', key: name };
  const alias = collection.aliases?.[name];
  if (!alias) return null;
  if (alias.parent && collection.icons?.[alias.parent]) {
    return { kind: 'alias', key: name, parent: alias.parent };
  }
  return null;
}

function buildSubset(collection, wantedNames) {
  const icons = {};
  const aliases = {};
  const missing = [];
  for (const name of [...wantedNames].sort()) {
    const resolved = resolveEntry(collection, name);
    if (!resolved) {
      missing.push(name);
      continue;
    }
    if (resolved.kind === 'icon') {
      icons[name] = collection.icons[name];
    } else {
      aliases[name] = collection.aliases[name];
      icons[resolved.parent] = collection.icons[resolved.parent];
    }
  }
  const subset = {
    prefix: collection.prefix,
    icons,
    lastModified: collection.lastModified,
    width: collection.width,
    height: collection.height,
  };
  if (Object.keys(aliases).length > 0) subset.aliases = aliases;
  return { subset, missing };
}

function renderIndexModule(collections) {
  const imports = collections
    .map((prefix, index) => `import ${collectionVar(index)} from './${prefix}.subset.json';`)
    .join('\n');
  const entries = collections.map((prefix, index) => `${collectionVar(index)}`).join(', ');
  const names = collections
    .map(
      (prefix, index) =>
        `  ...Object.keys(${collectionVar(index)}.icons || {}).map(name => '${prefix}:' + name),\n` +
        `  ...Object.keys(${collectionVar(index)}.aliases || {}).map(name => '${prefix}:' + name),`
    )
    .join('\n');

  return `// 由 tools/generate-iconify-subset.mjs 生成，请勿手改。
//
// 只包含源码里实际引用到的图标。用户自定义图标名若不在其中，
// 由调用方回退（见 pages/bookmarks/EngineIconPreview.jsx），
// 不要让 @iconify/react 去请求远端 API —— 内网/离线时那是静默失败。
${imports}

export const ICONIFY_COLLECTIONS = [${entries}];

const AVAILABLE_ICON_NAMES = new Set([
${names}
]);

/** 该 "集合:图标名" 是否已随构建产物打包。 */
export function hasIcon(name) {
  return AVAILABLE_ICON_NAMES.has(String(name ?? '').trim());
}
`;
}

function collectionVar(index) {
  return `collection${index}`;
}

function writeOrCheck(filePath, content) {
  const relPath = path.relative(rootDir, filePath);
  if (checkMode) {
    if (!existsSync(filePath)) {
      console.error(`缺失生成产物: ${relPath}（请运行 npm run icons:generate）`);
      return false;
    }
    if (readFileSync(filePath, 'utf8') !== content) {
      console.error(`生成产物已过期: ${relPath}（请运行 npm run icons:generate）`);
      return false;
    }
    return true;
  }
  mkdirSync(path.dirname(filePath), { recursive: true });
  writeFileSync(filePath, content, 'utf8');
  console.log(`写入 ${relPath}（${content.length} 字节）`);
  return true;
}

function main() {
  const prefixes = installedCollections();
  if (prefixes.size === 0) {
    console.warn('未安装任何 @iconify-json/* 包，跳过图标裁剪');
    return;
  }

  const used = collectUsedIcons(new Set(prefixes.keys()));
  if (used.size === 0) {
    console.warn('源码中未引用任何 Iconify 图标，保留已有产物');
    return;
  }

  const collections = [];
  let ok = true;
  let missingAll = [];

  for (const [prefix, names] of [...used].sort((a, b) => a[0].localeCompare(b[0]))) {
    const source = JSON.parse(readFileSync(prefixes.get(prefix), 'utf8'));
    const { subset, missing } = buildSubset(source, names);
    const iconCount = Object.keys(subset.icons).length;
    if (iconCount === 0) {
      console.warn(`集合 ${prefix} 未匹配到任何有效图标，跳过`);
      continue;
    }
    missingAll = missingAll.concat(missing.map(name => `${prefix}:${name}`));
    ok = writeOrCheck(path.join(outDir, `${prefix}.subset.json`), `${JSON.stringify(subset)}\n`) && ok;
    collections.push(prefix);
    console.log(
      `集合 ${prefix}: 引用 ${names.size} 个，打包 ${iconCount} 个` +
        (missing.length > 0 ? `，未找到 ${missing.join(', ')}` : '')
    );
  }

  if (collections.length === 0) {
    console.warn('没有可生成的集合');
    return;
  }

  ok = writeOrCheck(path.join(outDir, 'index.js'), renderIndexModule(collections)) && ok;

  if (missingAll.length > 0) {
    console.warn(
      `以下图标名在源码中出现但集合里不存在（多半是注释或占位示例，UI 会走文字兜底）：${missingAll.join(', ')}`
    );
  }

  if (!ok) process.exit(1);
}

main();
