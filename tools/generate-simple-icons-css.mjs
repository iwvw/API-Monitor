import { existsSync } from 'node:fs';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceDir = path.join(rootDir, 'src');
const outputPath = path.join(sourceDir, 'css', 'simple-icons.css');
const subsetFontPath = path.join(sourceDir, 'assets', 'fonts', 'SimpleIcons.subset.woff2');
const upstreamFontPath = path.join(
  rootDir,
  'node_modules',
  'simple-icons-font',
  'font',
  'SimpleIcons.woff2'
);
const upstreamCSSPath = path.join(
  rootDir,
  'node_modules',
  'simple-icons-font',
  'font',
  'simple-icons.css'
);
const sourceExtensions = new Set(['.js', '.jsx', '.css', '.html']);

/**
 * 调用 fontTools 做子集化，兼容两种安装形态：
 *  - pip 安装 → 有 `pyftsubset` 命令；
 *  - apt 安装 `python3-fonttools` → 只有 Python 模块，没有入口脚本
 *    （实测 Debian bookworm / node:20-slim 即如此），需走 `python3 -m fontTools.subset`。
 * 以前只认第一种，于是在 Debian 系上永远静默退回全量字体。
 */
function runFontSubset(args) {
  const attempts = [
    ['pyftsubset', args],
    ['python3', ['-m', 'fontTools.subset', ...args]],
  ];
  const failures = [];
  for (const [command, commandArgs] of attempts) {
    try {
      execFileSync(command, commandArgs, { stdio: 'pipe' });
      return;
    } catch (error) {
      if (error.code === 'ENOENT') {
        failures.push(`${command} 不存在`);
        continue;
      }
      failures.push(`${command}: ${String(error.message).split('\n')[0]}`);
    }
  }
  throw new Error(failures.join('; '));
}

async function collectSourceFiles(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map(async entry => {
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) return collectSourceFiles(fullPath);
      if (fullPath === outputPath || !sourceExtensions.has(path.extname(entry.name))) return [];
      return [fullPath];
    })
  );
  return nested.flat();
}

const files = await collectSourceFiles(sourceDir);
const usedClasses = new Set();
for (const file of files) {
  const source = await readFile(file, 'utf8');
  for (const match of source.matchAll(/\bsi-([a-z0-9][a-z0-9-]*)\b/g)) {
    if (match[1] !== 'fit') usedClasses.add(match[1]);
  }
}

const upstreamCSS = await readFile(upstreamCSSPath, 'utf8');
const rules = [];
const missing = [];
for (const name of [...usedClasses].sort()) {
  const escapedName = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const iconRule = upstreamCSS.match(
    new RegExp(`^\\.si-${escapedName}::before \\{[^\\n]+$`, 'm')
  )?.[0];
  const colorRule = upstreamCSS.match(
    new RegExp(`^\\.si-${escapedName}\\.si--color::before \\{[^\\n]+$`, 'm')
  )?.[0];
  if (!iconRule) {
    missing.push(name);
    continue;
  }
  rules.push(iconRule);
  if (colorRule) rules.push(colorRule);
}

// 从命中的 CSS 规则里提取码点，把 805KB 的完整字体子集化到仅含用到的图标。
const codepoints = new Set();
for (const rule of rules) {
  for (const match of rule.matchAll(/content:\s*"\\([0-9a-fA-F]+)"/g)) {
    codepoints.add(match[1]);
  }
}

const upstreamFontRelative = '../../node_modules/simple-icons-font/font/SimpleIcons.woff2';
let fontSrc = upstreamFontRelative;
if (codepoints.size > 0) {
  try {
    runFontSubset([
      upstreamFontPath,
      `--unicodes=${[...codepoints].map(cp => `U+${cp}`).join(',')}`,
      '--flavor=woff2',
      `--output-file=${subsetFontPath}`,
      '--no-hinting',
      '--desubroutinize',
    ]);
    fontSrc = '../assets/fonts/SimpleIcons.subset.woff2';
    console.log(`Subset font written to ${path.relative(rootDir, subsetFontPath)} (${codepoints.size} glyphs).`);
  } catch (error) {
    // 这里过去只 warn 再退回全量字体，代价是发布产物里多 800KB
    // （全量 819,196 B vs 子集 ~15KB），而且**没有任何信号**提示
    // 「子集化没生效」——实际线上一直如此。
    //
    // 现在的行为分三档：
    //   1. CI / STRICT_FONT_SUBSET=1：直接失败。缺依赖必须修，不能静默胖包。
    //   2. 本地且已有子集产物：**沿用已有子集**（不覆盖），只 warn。
    //      这样本地 `npm run build` 不会把已入库的子集字体改回 800KB，
    //      也不会把 CSS 指向 node_modules，避免每次构建都留下脏工作区。
    //   3. 本地且没有子集产物：才退回上游全量字体（此时 CSS 引用 node_modules）。
    const reason = String(error.message).split('\n')[0];
    const strict = process.env.CI === 'true' || process.env.STRICT_FONT_SUBSET === '1';
    if (strict) {
      console.error(
        `字体子集化失败（${reason}），但当前是严格模式：拒绝回退到 819KB 的全量字体。\n` +
          '  安装方式任选其一：\n' +
          '    apt install python3-fonttools python3-brotli   （无需 pip，脚本会用 python3 -m fontTools.subset）\n' +
          '    pip install "fonttools[woff2]"                  （提供 pyftsubset 命令）'
      );
      process.exit(1);
    }
    if (existsSync(subsetFontPath)) {
      fontSrc = '../assets/fonts/SimpleIcons.subset.woff2';
      console.warn(
        `字体子集化跳过 (${reason})；沿用已入库的 ${path.relative(rootDir, subsetFontPath)}。` +
          '若本次改动了图标集合，请安装 fonttools 后重跑 npm run icons:generate。'
      );
    } else {
      console.warn(
        `字体子集化失败 (${reason})，且没有可沿用的子集产物；回退到上游全量字体（体积约 800KB）。` +
          'CI 在严格模式下会直接失败。'
      );
    }
  }
}

const generated = `/* Generated by tools/generate-simple-icons-css.mjs. */
@font-face {
  font-family: 'Simple Icons';
  src: url('${fontSrc}') format('woff2');
  font-display: swap;
}

.si {
  font-style: normal;
  font-family: 'Simple Icons', sans-serif;
  vertical-align: middle;
}

${rules.join('\n')}
`;

await writeFile(outputPath, generated, 'utf8');
console.log(
  `Generated ${path.relative(rootDir, outputPath)} with ${rules.length} rules for ${usedClasses.size - missing.length} icons.`
);
if (missing.length > 0) {
  console.warn(`Simple Icons classes missing upstream mappings: ${missing.join(', ')}`);
}
