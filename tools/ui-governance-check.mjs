import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const scanRoots = ['src/js', 'src/css'];
const failures = [];
const warnings = [];
const allowedExceptions = [];

const skippedDirs = new Set(['.git', 'node_modules', 'dist', 'data', 'public']);

const rawControlRe = /<(button|select|input|textarea)\b/g;
const deprecatedMotionRe =
  /quick-fade-in|motion-pop-in|app-collapse-panel|transition-shadow/;
const legacyFrontendPatterns = [
  /\bcreateApp\s*\(/,
  /\bnew\s+Vue\b/,
  /\bVue\.component\b/,
  /from\s+['"]vue['"]/,
  /from\s+['"][^'"]+\.vue['"]/,
  /from\s+['"]pinia['"]/,
  /from\s+['"][^'"]*pinia[^'"]*['"]/,
  /chart\.js/i,
];
const hardcodedColorRe =
  /#[0-9A-Fa-f]{3,8}\b|\b(?:bg|text|border|ring|from|to|via)-(?:red|orange|amber|yellow|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone|black|white)(?:-[0-9]{2,3})?\b/g;
const destructiveConfirmRe = /\b(?:window\.)?confirm\(|dialog\.confirm\(/;
const destructiveWordsRe = /删除|delete|remove|destroy|purge/i;

function toPosix(file) {
  return file.split(path.sep).join('/');
}

function exists(rel) {
  return fs.existsSync(path.join(root, rel));
}

function walk(relDir, out = []) {
  const absDir = path.join(root, relDir);
  if (!fs.existsSync(absDir)) return out;

  for (const entry of fs.readdirSync(absDir, { withFileTypes: true })) {
    const rel = toPosix(path.join(relDir, entry.name));
    if (entry.isDirectory()) {
      if (!skippedDirs.has(entry.name) && !skippedDirs.has(rel)) walk(rel, out);
      continue;
    }
    if (/\.(js|jsx|mjs|cjs|css)$/.test(entry.name)) out.push(rel);
  }
  return out;
}

function readLines(rel) {
  return fs.readFileSync(path.join(root, rel), 'utf8').split(/\r?\n/);
}

function noteAllowed(rel, lineNumber, value, reason) {
  allowedExceptions.push(`${rel}:${lineNumber} ${value} (${reason})`);
}

function isAllowedRawControl(tag, line, lines, index) {
  const block = lines.slice(index, Math.min(lines.length, index + 12)).join(' ');
  if (tag === 'textarea' && /\bapp-code-editor-input\b/.test(block)) {
    return 'code editor transparent textarea overlay';
  }
  if (tag === 'textarea' && /\bapp-markdown-editor-input\b/.test(block)) {
    return 'markdown editor transparent textarea overlay';
  }
  if (tag === 'textarea' && /\bremote-system-keyboard-input\b/.test(block)) {
    return 'hidden native mobile system-keyboard bridge';
  }
  if (tag === 'button' && /\bh-\[30\.5px\].*shrink-0/.test(block)) {
    return 'custom compact table-row header toggler';
  }
  if (tag === 'button' && /\brounded-md border border-kumo-line/.test(block)) {
    return 'custom multi-line action row button';
  }
  if (tag === 'button' && /\brounded border px-2 py-0\.5/.test(block)) {
    return 'compact list row selectable button';
  }
  if (tag === 'label' && /\bapp-file-dropzone\b/.test(block)) {
    return 'file dropzone label overlay';
  }
  if (tag === 'label' && /\bapp-color-input-overlay\b/.test(block)) {
    return 'hidden native color input overlay';
  }
  if (tag !== 'input') return null;
  if (/type=["']file["']/.test(block) && /\b(hidden|sr-only)\b/.test(block)) {
    return 'hidden native file picker';
  }
  return null;
}

function allowedColorReason(rel, line, value) {
  if (rel === 'src/js/modules/pwa.js' && value.startsWith('#')) {
    return 'browser titlebar theme-color metadata';
  }
  if (rel === 'src/css/app.css' && line.includes('--app-main-bg')) {
    return 'app canvas color-mix fallback';
  }
  if (rel === 'src/js/components/ui/BrandIcon.jsx' && value.startsWith('#')) {
    return 'external brand identity color';
  }
  if (rel === 'src/css/simple-icons.css' && value.startsWith('#')) {
    return 'generated external brand identity color';
  }
  if (rel === 'src/css/app.css' && line.includes('--app-terminal-')) {
    return 'terminal fallback color';
  }
  if (rel === 'src/js/pages/ServerPage.jsx' && line.includes('--app-terminal-')) {
    return 'terminal fallback color';
  }
  if (rel === 'src/js/pages/UptimePage.jsx' && value.startsWith('#')) {
    return 'legacy ECharts color; migrate when touching uptime charts';
  }
  if (rel === 'src/js/pages/FileboxPage.jsx' && line.includes('color: { dark:')) {
    return 'QR code contrast color';
  }
  if (rel === 'src/js/pages/GitHubPage.jsx' && value === 'text-white' && line.includes('bg-kumo-danger')) {
    return 'danger confirm button contrast text';
  }
  if (rel === 'src/js/pages/FileboxPage.jsx' && value === 'bg-white' && line.includes('二维码')) {
    return 'QR code image background';
  }
  if (rel === 'src/js/pages/VoidRoomPage.jsx' && value === 'bg-white' && line.includes('二维码')) {
    return 'QR code image background';
  }
  if (rel === 'src/js/pages/TotpPage.jsx' && value === 'bg-black') {
    return 'camera/QR scanner surface';
  }
  if (rel === 'src/js/pages/TotpPage.jsx' && value.startsWith('#')) {
    return 'TOTP brand/icon color value example or fallback';
  }
  if (rel === 'src/js/pages/DnsPage.jsx' && (value === 'bg-black' || value === 'bg-white')) {
    return 'media preview surface';
  }
  if (rel === 'src/js/components/server/ServerLocationMap.jsx') {
    return 'map status and bubble styling colors';
  }
  if (rel === 'src/js/modules/publicPageBranding.js' && value.startsWith('#')) {
    return 'public page accent color preference store';
  }
  if (
    rel.endsWith('GitHubPage.jsx') &&
    line.includes('actionFlowStatusDotClass') &&
    /text-kumo-(success|danger|warning|info|line)/.test(line)
  ) {
    return 'action flow status dot semantic colors';
  }
  if (rel === 'src/js/pages/GitHubPage.jsx' && value === '#b8c2cf') {
    return 'workflow graph idle edge color';
  }
  if (rel === 'src/js/pages/GitHubPage.jsx' && value === '#6ea8ff') {
    return 'workflow graph active edge color';
  }
  if (rel === 'src/js/pages/PublicGitHubPage.jsx' && value === '#b8c2cf') {
    return 'workflow graph idle edge color';
  }
  if (rel === 'src/js/pages/PublicGitHubPage.jsx' && value === '#6ea8ff') {
    return 'workflow graph active edge color';
  }
  if (
    (rel.endsWith('GitHubPage.jsx') || rel.endsWith('PublicGitHubPage.jsx')) &&
    value.startsWith('#')
  ) {
    return 'status/coverage heart-map palette color';
  }
  if ((rel === 'src/js/pages/ApiDocsPage.jsx' || rel === 'src/js/pages/SettingsPage.jsx') && value === 'text-white') {
    return 'contrast text on colored status block';
  }
  if (rel === 'src/js/components/adminai/AskAiPanel.jsx' && value === 'bg-black') {
    return 'Ask AI 侧栏半透明遮罩（PRD 指定 bg-black/30）';
  }
  if (rel === 'src/js/components/adminai/MessageList.jsx' && value === 'text-white' && line.includes('bg-kumo-brand')) {
    return 'Ask AI 用户消息气泡对比文字';
  }
  return null;
}

function scanFile(rel) {
  const lines = readLines(rel);
  lines.forEach((line, index) => {
    const lineNumber = index + 1;

    for (const match of line.matchAll(rawControlRe)) {
      const reason = isAllowedRawControl(match[1], line, lines, index);
      if (reason) {
        noteAllowed(rel, lineNumber, match[0], reason);
      } else {
        failures.push(`${rel}:${lineNumber} raw <${match[1]}> control should use Kumo`);
      }
    }

    if (deprecatedMotionRe.test(line)) {
      failures.push(`${rel}:${lineNumber} deprecated self-drawn motion/shadow class`);
    }

    if (destructiveConfirmRe.test(line) && destructiveWordsRe.test(line)) {
      warnings.push(`${rel}:${lineNumber} destructive confirm should migrate toward dialog.deleteResource`);
    }

    for (const match of line.matchAll(hardcodedColorRe)) {
      const value = match[0];
      const reason = allowedColorReason(rel, line, value);
      if (reason) {
        noteAllowed(rel, lineNumber, value, reason);
      } else {
        failures.push(`${rel}:${lineNumber} hardcoded color "${value}" is not in the exception list`);
      }
    }
  });
}

function scanLegacyFrontend(files) {
  const packageFiles = ['package.json', 'package-lock.json'].filter(exists);
  for (const rel of [...files, ...packageFiles]) {
    const content = fs.readFileSync(path.join(root, rel), 'utf8');
    for (const pattern of legacyFrontendPatterns) {
      if (pattern.test(content)) {
        failures.push(`${rel}: legacy frontend stack reference matched ${pattern}`);
      }
    }
  }
}

function scanTableLayouts(files) {
  for (const rel of files.filter((file) => /\.jsx$/.test(file))) {
    const content = fs.readFileSync(path.join(root, rel), 'utf8');
    if (/<AppTable\b[^>]*\bpercentageWidths\b/.test(content)) {
      failures.push(`${rel}: percentageWidths is not allowed; use semantic columns with fixed utility roles`);
    }

    for (const match of content.matchAll(/<AppTable\b(?=[^>]*\bwidths\s*=)[^>]*>([\s\S]*?)<\/AppTable>/g)) {
      if (/<colgroup\b/.test(match[1])) continue;
      const lineNumber = content.slice(0, match.index).split(/\r?\n/).length;
      warnings.push(`${rel}:${lineNumber} legacy AppTable widths should migrate to semantic columns`);
    }

    for (const match of content.matchAll(/<Table\b(?=[^>]*\blayout=["']fixed["'])[^>]*>([\s\S]*?)<\/Table>/g)) {
      if (/<colgroup\b/.test(match[1]) || /<Table\.Head\b[^>]*\bw-/.test(match[1])) continue;
      const lineNumber = content.slice(0, match.index).split(/\r?\n/).length;
      warnings.push(`${rel}:${lineNumber} fixed Kumo Table has no semantic AppTable columns or colgroup`);
    }
  }
}

const files = scanRoots.flatMap((dir) => walk(dir));
for (const file of files) scanFile(file);
scanLegacyFrontend(files);
scanTableLayouts(files);

if (warnings.length) {
  console.log('UI governance warnings:');
  for (const warning of warnings) console.log(`  - ${warning}`);
  console.log('');
}

if (failures.length) {
  console.error('UI governance check failed:');
  for (const failure of failures) console.error(`  - ${failure}`);
  process.exit(1);
}

console.log(
  `UI governance check passed (${files.length} source files, ${allowedExceptions.length} documented exceptions).`,
);
