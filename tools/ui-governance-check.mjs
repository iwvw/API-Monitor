import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const scanRoots = ['src/js', 'src/css'];
const failures = [];
const warnings = [];
const allowedExceptions = [];

const skippedDirs = new Set(['.git', 'node_modules', 'dist', 'data']);

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
  /chart\.js(?!x)/i,
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
  if (tag === 'button' && /\brounded-xl border px-3 py-3/.test(block)) {
    return 'public page icon selectable card';
  }
  if (tag === 'button' && /\btriggerClassName\b/.test(block)) {
    return 'public page icon picker trigger';
  }
  if (tag === 'button' && /\bflex flex-col items-center gap-1\.5 rounded-lg border/.test(block)) {
    return 'icon+label selectable card';
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
  if (/type=["']color["']/.test(block) && /\b(hidden|sr-only)\b/.test(block)) {
    return 'hidden native color input';
  }
  return null;
}

function allowedColorReason(rel, line, value, lines, index) {
  if (rel === 'src/js/modules/pwa.js' && value.startsWith('#')) {
    return 'browser titlebar theme-color metadata';
  }
  if (rel === 'src/css/app.css' && line.includes('--app-main-bg')) {
    return 'app canvas color-mix fallback';
  }
  if (rel === 'src/js/components/ui/BrandIcon.jsx' && value.startsWith('#')) {
    return 'external brand identity color';
  }
  if (
    rel === 'src/js/components/Icons.jsx' &&
    ['#00c70a', '#37aee2', '#c8daea', '#a9c9dd', '#f6fbfe', '#0082ef', '#ffffff'].includes(value.toLowerCase())
  ) {
    return 'official WeChat / Telegram / WeCom brand mark colors (Icons.jsx brand components)';
  }
  if (rel === 'src/css/simple-icons.css' && value.startsWith('#')) {
    return 'generated external brand identity color';
  }
  if (rel === 'src/css/app.css' && line.includes('--app-terminal-')) {
    return 'terminal fallback color';
  }
  if (rel === 'src/css/app.css' && (line.includes('--color-brand') || line.includes('#dc7d40') || line.includes('#fb923c'))) {
    return 'site brand token definition in @theme inline';
  }
  if (rel === 'src/js/pages/ServerPage.jsx' && line.includes('--app-terminal-')) {
    return 'terminal fallback color';
  }
  if (rel === 'src/js/pages/huawei/SSHTerminalDialog.jsx' && value.startsWith('#')) {
    return 'SSH 终端 xterm 兜底颜色（docs/standards/重构验证与例外清单.md 登记）';
  }
  if (rel === 'src/js/pages/UptimePage.jsx' && value.startsWith('#')) {
    return 'legacy ECharts color; migrate when touching uptime charts';
  }
  if (rel === 'src/js/pages/filebox/FileboxPage.jsx' && line.includes('color: { dark:')) {
    return 'QR code contrast color';
  }
  if (rel === 'src/js/pages/github/GitHubPage.jsx' && value === 'text-white' && line.includes('bg-kumo-danger')) {
    return 'danger confirm button contrast text';
  }
  if (rel === 'src/js/pages/filebox/SharePanel.jsx' && value === 'bg-white' && line.includes('二维码')) {
    return 'QR code image background';
  }
  if (rel === 'src/js/pages/VoidRoomPage.jsx' && value === 'bg-white' && line.includes('二维码')) {
    return 'QR code image background';
  }
  if (rel === 'src/js/pages/totp/AccountDialog.jsx' && value === 'bg-black') {
    return 'camera/QR scanner surface';
  }
  if (
    (rel === 'src/js/pages/totp/TotpBrandMark.jsx' ||
      rel === 'src/js/pages/totp/utils.js' ||
      rel === 'src/js/pages/totp/constants.js' ||
      rel === 'src/js/pages/totp/AccountDialog.jsx' ||
      rel === 'src/js/pages/totp/GroupDialog.jsx') &&
    value.startsWith('#')
  ) {
    return 'TOTP brand/icon color value example or fallback';
  }
  if ((rel === 'src/js/pages/DnsPage.jsx' || rel === 'src/js/pages/dns/R2PreviewDialog.jsx') && (value === 'bg-black' || value === 'bg-white')) {
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
  if ((rel === 'src/js/pages/GitHubPage.jsx' || rel === 'src/js/pages/github/ActionWorkflowCanvas.jsx') && value === '#b8c2cf') {
    return 'workflow graph idle edge color';
  }
  if ((rel === 'src/js/pages/GitHubPage.jsx' || rel === 'src/js/pages/github/ActionWorkflowCanvas.jsx') && value === '#6ea8ff') {
    return 'workflow graph active edge color';
  }
  if ((rel === 'src/js/pages/PublicGitHubPage.jsx' || rel === 'src/js/pages/public-github/ActionWorkflowCanvas.jsx') && value === '#b8c2cf') {
    return 'workflow graph idle edge color';
  }
  if ((rel === 'src/js/pages/PublicGitHubPage.jsx' || rel === 'src/js/pages/public-github/ActionWorkflowCanvas.jsx') && value === '#6ea8ff') {
    return 'workflow graph active edge color';
  }
  if (
    (rel.endsWith('GitHubPage.jsx') || rel.endsWith('PublicGitHubPage.jsx')) &&
    value.startsWith('#')
  ) {
    return 'status/coverage heart-map palette color';
  }
  if ((rel === 'src/js/pages/apidocs/ApiDocsPage.jsx' || rel === 'src/js/pages/SettingsPage.jsx' || rel === 'src/js/pages/settings/SettingsPage.jsx') && value === 'text-white') {
    return 'contrast text on colored status block';
  }
  if (rel === 'src/js/components/adminai/AskAiPanel/index.jsx' && value === 'bg-black') {
    return 'Ask AI 侧栏半透明遮罩（PRD 指定 bg-black/30）';
  }
  if (rel === 'src/js/components/adminai/AskAiPanel/MessageList.jsx' && (value === 'text-white' || value === 'bg-white')) {
    const blockStart = Math.max(0, index - 40);
    const block = lines.slice(blockStart, index + 1).join('\n');
    if (block.includes('from-brand') || block.includes('from-kumo-brand') || block.includes('editing && editing.id === msg.id')) {
      return 'Ask AI 用户消息气泡（含编辑态）对比文字';
    }
  }
  if (rel === 'src/js/components/adminai/primitives/MessageBubble.jsx' && value === 'text-white') {
    return 'Ask AI 用户消息气泡（user variant）对比文字';
  }
  if (rel === 'src/js/components/adminai/AskAiPanel/ApprovalCard.jsx' && value === 'text-white' && line.includes('bg-kumo-success')) {
    return 'Ask AI 批准按钮白色对比文字';
  }
  if (rel === 'src/js/components/adminai/AskAiPanel/index.jsx' && value === 'text-white' && line.includes('from-kumo-brand')) {
    return 'Ask AI 发送按钮白色对比文字';
  }
  if (
    rel === 'src/js/components/MainLayout.jsx' &&
    (value === '#dc7d40' || (value === 'text-white' && line.includes('askai-entry-sparkle')))
  ) {
    return 'Ask AI 入口按钮品牌橙（docs/standards/重构验证与例外清单.md 登记）';
  }
  if (rel === 'src/css/app.css' && (value === '#FFF' || value === '#FFEDDD' || value === '#FF9335' || value === '#FFB371')) {
    return 'Ask AI 云朵动画 Cloudflare 品牌色';
  }
  if (rel === 'src/js/pages/BookmarksPage.jsx' && value === 'text-white') {
    return '网址文字图标对比色（用户自定义背景上保证可读）';
  }
  if (rel === 'src/js/pages/BookmarksPage.jsx' && value === '#808080') {
    return '原生颜色选择器中性占位值（空背景时）';
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
      const reason = allowedColorReason(rel, line, value, lines, index);
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

// 表格布局迁移基线：数值只允许下降。详见 tools/table-layout-baseline.json。
// 新增违规（超过基线）即失败；减少后需同步下调基线文件。
function loadTableBaseline() {
  const baselinePath = path.join(root, 'tools', 'table-layout-baseline.json');
  if (!fs.existsSync(baselinePath)) return null;
  try {
    return JSON.parse(fs.readFileSync(baselinePath, 'utf8'));
  } catch {
    return null;
  }
}

function scanTableLayouts(files) {
  const baseline = loadTableBaseline();
  const counts = { appTableWidths: 0, bareFixedTables: 0, headWidthClasses: 0, percentCols: 0 };

  for (const rel of files.filter((file) => /\.jsx$/.test(file))) {
    const content = fs.readFileSync(path.join(root, rel), 'utf8');
    const lineOf = (index) => content.slice(0, index).split(/\r?\n/).length;

    if (/<AppTable\b[^>]*\bpercentageWidths\b/.test(content)) {
      failures.push(`${rel}: percentageWidths is not allowed; use semantic columns with fixed utility roles`);
    }

    // 旧 widths API：应为 0，全部迁移到语义 columns。
    for (const match of content.matchAll(/<AppTable\b[^>]*\bwidths\s*=/g)) {
      counts.appTableWidths += 1;
      if (baseline && counts.appTableWidths > baseline.appTableWidths) {
        failures.push(`${rel}:${lineOf(match.index)} legacy AppTable widths must migrate to semantic columns (baseline ${baseline.appTableWidths})`);
      }
    }

    // 裸固定表：应改为语义 columns。
    for (const match of content.matchAll(/<Table\b[^>]*\blayout=["']fixed["']/g)) {
      counts.bareFixedTables += 1;
      if (baseline && counts.bareFixedTables > baseline.bareFixedTables) {
        failures.push(`${rel}:${lineOf(match.index)} fixed Kumo Table must use semantic AppTable columns (baseline ${baseline.bareFixedTables})`);
      }
    }

    // 表头硬编码宽度类：应改为语义列的固定宽度。
    for (const match of content.matchAll(/<Table\.Head\b[^>]*\b!?w-/g)) {
      counts.headWidthClasses += 1;
      if (baseline && counts.headWidthClasses > baseline.headWidthClasses) {
        failures.push(`${rel}:${lineOf(match.index)} Table.Head hardcoded width class should use semantic column roles (baseline ${baseline.headWidthClasses})`);
      }
    }

    // 手写百分比列宽：规范禁止，迁移期间按基线门禁，目标清零。
    for (const match of content.matchAll(/<col\b[^>]*className=["'][^"']*w-\[\d+(?:\.\d+)?%\]/g)) {
      counts.percentCols += 1;
      if (baseline && counts.percentCols > baseline.percentCols) {
        failures.push(`${rel}:${lineOf(match.index)} percentage column width is not allowed; use semantic column roles (baseline ${baseline.percentCols})`);
      }
    }
  }

  if (baseline) {
    warnings.push(
      `table layout migration progress: appTableWidths=${counts.appTableWidths}/${baseline.appTableWidths}, ` +
        `bareFixedTables=${counts.bareFixedTables}/${baseline.bareFixedTables}, ` +
        `headWidthClasses=${counts.headWidthClasses}/${baseline.headWidthClasses}, ` +
        `percentCols=${counts.percentCols}/${baseline.percentCols}`
    );
  }
}

const LAYER_DIALOG_SLOTS = [
  'LayerDialog.Title',
  'LayerDialog.Description',
  'LayerDialog.Body',
  'LayerDialog.Actions',
];

function findTagEnd(src, start) {
  let depth = 0;
  let i = start;
  while (i < src.length) {
    const ch = src[i];
    if (ch === '{') depth++;
    else if (ch === '}') depth--;
    else if (ch === '>' && depth === 0) return i;
    else if (ch === '"' || ch === "'" || ch === '`') {
      const quote = ch;
      i++;
      while (i < src.length && src[i] !== quote) {
        if (src[i] === '\\') i++;
        i++;
      }
    }
    i++;
  }
  return -1;
}

function findMatchingClose(src, tagStart, tagName) {
  const esc = tagName.replace(/\./g, '\\.');
  const openRe = new RegExp(`<${esc}(?=[\\s>])`, 'g');
  const closeRe = new RegExp(`</${esc}>`, 'g');
  let depth = 0;
  let i = tagStart;
  while (i < src.length) {
    openRe.lastIndex = i;
    closeRe.lastIndex = i;
    const open = openRe.exec(src);
    const close = closeRe.exec(src);
    if (!close) return -1;
    if (open && open.index < close.index) {
      depth++;
      i = open.index + 1;
    } else {
      depth--;
      if (depth === 0) return close.index + close[0].length;
      i = close.index + 1;
    }
  }
  return -1;
}

function stripBalancedBraces(src) {
  let out = '';
  let i = 0;
  while (i < src.length) {
    if (src[i] === '{') {
      let depth = 1;
      i++;
      while (i < src.length && depth > 0) {
        if (src[i] === '{') depth++;
        else if (src[i] === '}') depth--;
        i++;
      }
    } else {
      out += src[i];
      i++;
    }
  }
  return out;
}

// topLevelBraces 提取源码中所有顶层（非嵌套）的 { ... } 块内容。
// 用于检测 JSX 表达式是否包裹了 LayerDialog 的 slot 组件。
function topLevelBraces(src) {
  const blocks = [];
  let i = 0;
  while (i < src.length) {
    if (src[i] === '{') {
      let depth = 1;
      let j = i + 1;
      while (j < src.length && depth > 0) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') depth--;
        j++;
      }
      if (depth === 0) blocks.push(src.slice(i + 1, j - 1));
      i = j;
    } else {
      i++;
    }
  }
  return blocks;
}

// kumo 的 LayerDialog slot（Content/Title/Description/Body）只解构固定字段，
// className/ref 等属性会被静默丢弃。静默丢弃比报错更难发现
// （例如 Body 上的 gap-3 从未生效，视觉上各行贴在一起），因此这里在构建前拦下。
function checkDroppedLayerDialogAttrs(rel, openTag, slotName, lineNumber) {
  for (const attr of ['className', 'ref']) {
    if (new RegExp(`\\b${attr}\\s*=`).test(openTag)) {
      failures.push(
        `${rel}:${lineNumber} ${slotName} drops "${attr}" at runtime (kumo only reads its own fields); move it to an inner element`
      );
    }
  }
}

// LayerDialog 是结构化弹窗：Content 的直接子元素只允许 Title/Body（各一个）与可选的
// Description/Actions；Actions 只能含一个 Primary。违反会在运行时抛错（kumo 源码硬校验），
// 因此这里在构建前把守，避免迁移或新增时又写出会崩的结构。
function scanLayerDialogs(files) {
  for (const rel of files.filter((file) => /\.jsx$/.test(file))) {
    const content = fs.readFileSync(path.join(root, rel), 'utf8');
    if (!content.includes('LayerDialog')) continue;

    const contentRe = /<LayerDialog\.Content(?=[\s>])/g;
    let match;
    while ((match = contentRe.exec(content))) {
      const tagEnd = findTagEnd(content, match.index);
      if (tagEnd < 0) continue;
      const closeEnd = findMatchingClose(content, match.index, 'LayerDialog.Content');
      if (closeEnd < 0) continue;
      let rest = content.slice(tagEnd + 1, closeEnd - '</LayerDialog.Content>'.length);

      // Content 自身同样只解构固定字段，className/ref 会被静默丢弃。
      checkDroppedLayerDialogAttrs(
        rel,
        content.slice(match.index, tagEnd + 1),
        'LayerDialog.Content',
        content.slice(0, match.index).split(/\r?\n/).length
      );

      // 先于 slot 剔除做检查：若某个顶层 {} 块内出现 Title/Body/Description 标签，
      // 说明 slot 被条件表达式包裹。Children.toArray 不展开顶层 Fragment，
      // kumo 的 collectSlot 会把它判为非法子元素并在运行时抛错。
      // （Actions 不参与判断：Actions.Primary 可以合法地出现在 Body 内部的三元里。）
      const slotMarkers = /<LayerDialog\.(Title|Body|Description)(?=[\s>])/;
      let wrapped = false;
      for (const block of topLevelBraces(rest)) {
        if (slotMarkers.test(block)) {
          wrapped = true;
          break;
        }
      }
      if (wrapped) {
        const lineNumber = content.slice(0, match.index).split(/\r?\n/).length;
        failures.push(
          `${rel}:${lineNumber} LayerDialog slots must not be wrapped in a JSX expression; move the condition outside LayerDialog.Content`
        );
      }

      for (const slot of LAYER_DIALOG_SLOTS) {
        const slotRe = new RegExp(`<${slot.replace(/\./g, '\\.')}(?=[\\s>])`, 'g');
        let slotMatch;
        while ((slotMatch = slotRe.exec(rest))) {
          const slotClose = findMatchingClose(rest, slotMatch.index, slot);
          if (slotClose < 0) break;
          const slotTagEnd = findTagEnd(rest, slotMatch.index);
          if (slotTagEnd > 0) {
            const lineNumber =
              content.slice(0, match.index).split(/\r?\n/).length +
              rest.slice(0, slotMatch.index).split(/\r?\n/).length -
              1;
            checkDroppedLayerDialogAttrs(
              rel,
              rest.slice(slotMatch.index, slotTagEnd + 1),
              slot,
              lineNumber
            );
          }
          rest = rest.slice(0, slotMatch.index) + ' '.repeat(slotClose - slotMatch.index) + rest.slice(slotClose);
          slotRe.lastIndex = slotMatch.index + 1;
        }
      }

      const leftover = rest.replace(/\{[\s\S]*?\}/g, ' ').replace(/\s+/g, ' ').trim();
      if (leftover) {
        const lineNumber = content.slice(0, match.index).split(/\r?\n/).length;
        failures.push(
          `${rel}:${lineNumber} LayerDialog.Content may only contain Title/Description/Body/Actions, found "${leftover.slice(0, 60)}"`
        );
      }
      contentRe.lastIndex = closeEnd;
    }

    const actionsRe = /<LayerDialog\.Actions(?=[\s>])/g;
    while ((match = actionsRe.exec(content))) {
      const tagEnd = findTagEnd(content, match.index);
      if (tagEnd < 0) continue;
      const closeEnd = findMatchingClose(content, match.index, 'LayerDialog.Actions');
      if (closeEnd < 0) continue;
      let rest = content.slice(tagEnd + 1, closeEnd - '</LayerDialog.Actions>'.length);
      let changed = true;
      while (changed) {
        changed = false;
        const primary = /<LayerDialog\.Actions\.Primary(?=[\s>])/.exec(rest);
        if (primary) {
          const primaryClose = findMatchingClose(rest, primary.index, 'LayerDialog.Actions.Primary');
          if (primaryClose < 0) break;
          rest = rest.slice(0, primary.index) + rest.slice(primaryClose);
          changed = true;
        }
      }
      const leftover = stripBalancedBraces(rest).replace(/\s+/g, ' ').trim();
      if (leftover) {
        const lineNumber = content.slice(0, match.index).split(/\r?\n/).length;
        failures.push(
          `${rel}:${lineNumber} LayerDialog.Actions may only contain one Actions.Primary, found "${leftover.slice(0, 60)}"`
        );
      }
      actionsRe.lastIndex = closeEnd;
    }
  }
}

const files = scanRoots.flatMap((dir) => walk(dir));
for (const file of files) scanFile(file);
scanLegacyFrontend(files);
scanTableLayouts(files);
scanLayerDialogs(files);

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
