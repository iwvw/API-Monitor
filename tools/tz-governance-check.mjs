import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

// 全站时区治理：以 user_settings.time_zone 为唯一控制点（timeutil 门面）。
// 扫描 backend-go 业务代码，拦截「调度/日历边界」类时区回退，防止未来新增功能
// 再次按容器系统时区（UTC）跑 cron 或构造日期归属。
//
// 分层：
//   - ERROR：cron 调度器未用 timeutil 站点时区（cron.New 无 WithLocation）。
//   - WARNING：可疑的 UTC/本地日历构造，需人工核对是否有日期归属语义。
//
// 绝对时刻存储（time.Now().UTC().Format(RFC3339) 写库/日志）属正确用法，不拦截。

const root = process.cwd();
const errors = [];
const warnings = [];

const skipDirs = new Set(['.git', 'node_modules', 'data', 'data2', 'tmp', 'dist', 'target']);

// 已知合理项：云 API 规范 / 外部平台默认值，虽用 UTC 日期但无面板「日历归属」语义。
const allowedDateOnlyFiles = new Set([
  'backend-go/internal/tencent/service.go', // TC3-HMAC 签名规范要求 UTC 日期
  'backend-go/internal/cloudflare/service.go', // Workers compatibility_date 平台默认值
]);

function walk(dir, out = []) {
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!skipDirs.has(entry.name)) walk(full, out);
      continue;
    }
    if (entry.name.endsWith('.go')) out.push(full);
  }
  return out;
}

function rel(full) {
  return path.relative(root, full).split(path.sep).join('/');
}

function stripLineComments(content) {
  return content.replace(/^\s*\/\/.*$/gm, '');
}

let files = walk(path.join(root, 'backend-go')).map(rel);
files = files.filter((short) => short.endsWith('.go') && !short.endsWith('_test.go'));
files = files.filter((short) => !short.startsWith('backend-go/internal/timeutil/'));

for (const short of files) {
  const content = fs.readFileSync(path.join(root, short), 'utf8');
  const code = stripLineComments(content);

  // 硬规则：任何 cron 调度器必须用 cron.WithLocation 绑定站点时区。
  for (const match of code.matchAll(/cron\.New\([^)]*\)/g)) {
    const call = match[0];
    if (call.includes('WithLocation')) continue;
    errors.push(`scheduler must pin site timezone via cron.WithLocation(${short}): ${call}`);
  }

  // 日历边界在 UTC 构造（月/日界、Truncate 日界）。
  if (/time\.Date\([^)]*time\.UTC\)/.test(code)) {
    warnings.push(`calendar boundary constructed in UTC (${short}): time.Date(..., time.UTC)`);
  }
  // 按「今天」取日期但未绑定站点时区。
  if (/\.UTC\(\)\.Format\("2006-01-02"\)/.test(code) && !allowedDateOnlyFiles.has(short)) {
    warnings.push(`date-only format uses UTC clock (${short}): .UTC().Format("2006-01-02")`);
  }
  if (/time\.Now\(\)\.Format\("2006-01-02"\)/.test(code)) {
    warnings.push(`date-only format uses server-local clock, should In(siteLoc) (${short}): time.Now().Format("2006-01-02")`);
  }
}

if (warnings.length) {
  console.log('Timezone governance warnings (review whether calendar semantics intended):');
  for (const warning of warnings) console.log(`  - ${warning}`);
  console.log('');
}

if (errors.length) {
  console.error('Timezone governance check failed:');
  for (const error of errors) console.error(`  - ${error}`);
  process.exit(1);
}

console.log(`Timezone governance check passed (${files.length} Go source files).`);