import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

// 动效治理：全站过渡/动画统一引用 motion token，禁止裸写毫秒数与自定义缓动。
//
// token 定义（src/css/app.css）：
//   :root      --motion-duration-{quick,base,medium,slow,slower} / --motion-ease-{soft,snappy,out,panel}
//   @theme     --transition-duration-* / --ease-*  → 生成 Tailwind duration-* / ease-* 工具类
//
// 分层（参考 transitions.dev 的 refine：只读扫描，报告 token 可落位处）：
//   - ERROR：AI 面板自有面（src/js/components/adminai/**、app.css 的 askai-* 块）必须用 token。
//   - WARNING：其余区域的既有硬编码时长，供增量迁移，不阻塞构建。
//
// 周期性循环动画（含 infinite）的时长是循环周期而非运动时长，不纳入检查；
// 一次性过渡/入场动画必须用 token。

const root = process.cwd();
const errors = [];
const warnings = [];

const skipDirs = new Set(['.git', 'node_modules', 'dist', 'data', 'target', '.cache', '.tmp']);
const AI_PANEL_DIR = 'src/js/components/adminai/';

const RAW_DURATION_RE = /\b\d+(?:\.\d+)?m?s\b/g;
const RAW_CUBIC_BEZIER_RE = /cubic-bezier\([^)]*\)/g;
// Tailwind 工具类里的裸时长/裸缓动：duration-200、duration-[350ms]、ease-[cubic-bezier(...)]
const RAW_DURATION_CLASS_RE = /\bduration-(?:\[[^\]]+\]|\d+)\b/g;
const RAW_EASE_CLASS_RE = /\bease-\[[^\]]+\]/g;
// 允许的时长工具类名（token 派生）与零值
const TOKEN_DURATION_CLASSES = new Set([
  'duration-quick',
  'duration-base',
  'duration-medium',
  'duration-slow',
  'duration-slower',
  'duration-0',
]);

function walk(relDir, out = []) {
  const absDir = path.join(root, relDir);
  if (!fs.existsSync(absDir)) return out;
  for (const entry of fs.readdirSync(absDir, { withFileTypes: true })) {
    const rel = `${relDir}/${entry.name}`;
    if (entry.isDirectory()) {
      if (!skipDirs.has(entry.name)) walk(rel, out);
      continue;
    }
    if (/\.(js|jsx|mjs|cjs)$/.test(entry.name)) out.push(rel);
  }
  return out;
}

function isAiPanelFile(rel) {
  return rel.startsWith(AI_PANEL_DIR);
}

// 找出 app.css 中 askai-* 相关规则块的起止范围，用于判定「AI 面板自有面」。
// 按大括号深度跟踪，兼容单行规则（如 `.askai-x:nth-child(2) { animation-delay: .15s; }`）。
function askaiBlockLineRanges(lines) {
  const ranges = [];
  let start = null;
  let depth = 0;
  lines.forEach((line, i) => {
    const lineNumber = i + 1;
    const code = stripComments(line);
    const opens = (code.match(/\{/g) || []).length;
    const closes = (code.match(/\}/g) || []).length;
    if (start === null && opens > 0 && /askai-[a-z0-9-]+/.test(code)) {
      start = lineNumber;
      depth = 0;
    }
    if (start === null) return;
    depth += opens - closes;
    if (depth <= 0) {
      ranges.push([start, lineNumber]);
      start = null;
    }
  });
  if (start !== null) ranges.push([start, lines.length]);
  return ranges;
}

function inRanges(lineNumber, ranges) {
  return ranges.some(([from, to]) => lineNumber >= from && lineNumber <= to);
}

function stripComments(line) {
  return line.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/, '');
}

function checkJsxFile(rel) {
  const lines = fs.readFileSync(path.join(root, rel), 'utf8').split(/\r?\n/);
  const isAi = isAiPanelFile(rel);
  lines.forEach((line, i) => {
    const lineNumber = i + 1;
    const code = stripComments(line);
    const report = (message) => {
      const entry = `${rel}:${lineNumber} ${message}`;
      if (isAi) errors.push(entry);
      else warnings.push(entry);
    };

    for (const match of code.matchAll(RAW_DURATION_CLASS_RE)) {
      const cls = match[0];
      if (TOKEN_DURATION_CLASSES.has(cls)) continue;
      report(`raw duration class "${cls}" should use duration-{quick,base,medium,slow,slower}`);
    }
    for (const match of code.matchAll(RAW_EASE_CLASS_RE)) {
      report(`raw ease class "${match[0]}" should use ease-{soft,snappy,panel}`);
    }
  });
}

function checkCssFile(rel) {
  const lines = fs.readFileSync(path.join(root, rel), 'utf8').split(/\r?\n/);
  const ranges = askaiBlockLineRanges(lines);
  // token 定义区（:root 的 --motion-* 与 @theme 的 --transition-duration-*/--ease-*）不检查自身取值
  let inTokenBlock = false;

  lines.forEach((line, i) => {
    const lineNumber = i + 1;
    const code = stripComments(line);
    if (/^\s*--motion-(duration|ease)-/.test(code) || /^\s*--(transition-duration|ease)-/.test(code)) {
      return;
    }
    if (/^\s*:\s*(root|@theme)/.test(code) || /@theme\b/.test(code)) inTokenBlock = true;
    if (/^\}/.test(code.trim())) inTokenBlock = false;
    if (inTokenBlock) return;

    const isAi = inRanges(lineNumber, ranges);
    const isMotionDecl = /(?:^|[\s;])(transition|animation)(?:-[a-z]+)?\s*:/.test(code);
    if (!isMotionDecl) return;
    // animation-delay 是错峰偏移量，不是运动时长；delay/duration 同属独立量，不纳入检查。
    if (/animation-delay\s*:/.test(code)) return;
    if (code.includes('infinite')) return;
    if (code.includes('none')) return;

    const report = (message) => {
      const entry = `${rel}:${lineNumber} ${message}`;
      if (isAi) errors.push(entry);
      else warnings.push(entry);
    };

    for (const match of code.matchAll(RAW_DURATION_RE)) {
      report(`raw duration "${match[0]}" should use var(--motion-duration-*)`);
    }
    for (const match of code.matchAll(RAW_CUBIC_BEZIER_RE)) {
      report(`raw easing "${match[0]}" should use var(--motion-ease-*)`);
    }
  });
}

const jsFiles = walk('src/js');
for (const file of jsFiles) checkJsxFile(file);
checkCssFile('src/css/app.css');

if (warnings.length) {
  console.log(`Motion governance warnings (${warnings.length} pre-existing, incremental migration):`);
  for (const warning of warnings) console.log(`  - ${warning}`);
  console.log('');
}

if (errors.length) {
  console.error('Motion governance check failed:');
  for (const error of errors) console.error(`  - ${error}`);
  process.exit(1);
}

console.log(
  `Motion governance check passed (${jsFiles.length} JS sources; AI panel surface tokenized, ${warnings.length} warnings elsewhere).`,
);
