import { copyFileSync, existsSync, mkdirSync, statSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';

const root = process.cwd();
const source = path.join(root, 'agent-rust', 'target', 'release', 'api-monitor-agent.exe');
const targetDir = path.join(root, 'public', 'agent');
const target = path.join(targetDir, 'agent-windows-amd64.exe');

if (!existsSync(source)) {
  throw new Error(`未找到 release 产物：${source}\n请先运行 npm run agent:build:windows`);
}

const version = spawnSync(source, ['--version'], { encoding: 'utf8' });
const reported = version.status === 0 ? version.stdout.trim() : '';
if (!/^api-monitor-agent\s+\S+$/.test(reported)) {
  throw new Error(`产物不是有效的 Agent 二进制：${reported || '缺少版本输出'}`);
}

mkdirSync(targetDir, { recursive: true });
copyFileSync(source, target);

const sourceSize = statSync(source).size;
const targetSize = statSync(target).size;
if (sourceSize !== targetSize) {
  throw new Error(`复制后大小不一致：源 ${sourceSize} 字节，目标 ${targetSize} 字节`);
}

process.stdout.write(`已更新 ${path.relative(root, target)}（${reported}，${targetSize} 字节）\n`);
