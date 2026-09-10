import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = path.join(process.cwd(), 'docs');
const broken = [];
const checked = [];

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(abs, out);
    else if (entry.name.endsWith('.md')) out.push(abs);
  }
  return out;
}

for (const file of walk(root)) {
  const content = fs.readFileSync(file, 'utf8');
  const rel = path.relative(process.cwd(), file).split(path.sep).join('/');
  for (const m of content.matchAll(/\]\(([^)\s]+)\)/g)) {
    const raw = m[1];
    if (/^(https?:|mailto:|#)/.test(raw)) continue;
    const [target] = raw.split('#');
    if (!target || !/\.(md|json)$/i.test(target)) continue;
    const decoded = decodeURIComponent(target);
    const resolved = path.resolve(path.dirname(file), decoded);
    checked.push(`${rel} -> ${raw}`);
    if (!fs.existsSync(resolved)) {
      broken.push({ file: rel, link: raw, expected: path.relative(process.cwd(), resolved).split(path.sep).join('/') });
    }
  }
}

console.log(`Docs link check: ${checked.length} internal links checked, ${broken.length} broken.`);
if (broken.length) {
  console.log('');
  for (const b of broken) console.log(`  BROKEN  ${b.file}\n          link: ${b.link}\n          resolves to: ${b.expected}`);
  process.exit(1);
}
