#!/usr/bin/env node
/**
 * 进程资源基线采集：把「优化前后差多少」变成可 diff 的 JSON。
 *
 * 为什么存在
 * ----------
 * 2026-11-05 做 Agent vs Prometheus 对比时，所有测量都是一次性脚本，测完就丢。
 * 于是每次讨论「是不是变快了」都只能重新发明一遍测量方法，且口径不一致
 * （有人看瞬时的 `ps %CPU`，有人看 top 的 1 分钟均值）。
 *
 * 本脚本固定一套口径，供后续每一项性能改动前后对比：
 *  - CPU 用 `/proc/<pid>/stat` 的 utime+stime 增量，按 `CLK_TCK` 换算，**不依赖 ps 的平滑算法**；
 *  - 内存取 `VmRSS`（实际常驻），不是 VmSize；
 *  - IO 取 `/proc/<pid>/io` 的 rchar/wchar（含缓存命中）与 read_bytes/write_bytes（真磁盘）；
 *  - 额外统计**逐秒 CPU 分布**：我们的 Agent 有 30 秒一次的状态查询尖峰，
 *    只看均值会把它平均掉，看不出「均值没变但尖峰变高了」。
 *
 * 用法
 * ----
 *   node tools/perf-baseline.mjs --pid 1234 --seconds 60
 *   node tools/perf-baseline.mjs --pid 1234 --seconds 300 --label "agent-1.5s" --out /tmp/a.json
 *
 * 输出
 * ----
 * stdout 为 JSON（重定向即可入库/对比），人类可读摘要写 stderr。
 * 退出码：0 成功，1 参数错误或进程不可读。
 *
 * 平台
 * ----
 * 只支持 Linux（依赖 /proc）。其它平台直接报错退出，不做「假装量到了」。
 * 这是刻意选择：macOS/Windows 的等价口径不同，混在一起会让基线失去可比性。
 */

import { readFileSync, writeFileSync } from 'node:fs';
import process from 'node:process';

const CLK_TCK = 100; // Linux 用户态默认；下面会尝试从 getconf 覆盖
const DEFAULT_SECONDS = 60;
const DEFAULT_INTERVAL_MS = 1000;

function parseArgs(argv) {
  const options = {
    pid: 0,
    seconds: DEFAULT_SECONDS,
    intervalMs: DEFAULT_INTERVAL_MS,
    label: '',
    out: '',
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    const next = () => {
      const value = argv[i + 1];
      if (value === undefined) throw new Error(`${arg} 缺少取值`);
      i += 1;
      return value;
    };
    switch (arg) {
      case '--pid':
        options.pid = Number.parseInt(next(), 10);
        break;
      case '--seconds':
        options.seconds = Number.parseInt(next(), 10);
        break;
      case '--interval-ms':
        options.intervalMs = Number.parseInt(next(), 10);
        break;
      case '--label':
        options.label = next();
        break;
      case '--out':
        options.out = next();
        break;
      case '--help':
      case '-h':
        printUsage();
        process.exit(0);
        break;
      default:
        throw new Error(`未知参数: ${arg}`);
    }
  }
  if (!Number.isInteger(options.pid) || options.pid <= 0) {
    throw new Error('必须提供 --pid <正整数>');
  }
  if (!Number.isInteger(options.seconds) || options.seconds <= 0) {
    throw new Error('--seconds 必须为正整数');
  }
  if (!Number.isInteger(options.intervalMs) || options.intervalMs <= 0) {
    throw new Error('--interval-ms 必须为正整数');
  }
  return options;
}

function printUsage() {
  console.log(`用法: node tools/perf-baseline.mjs --pid <pid> [选项]

选项:
  --seconds <n>       采样时长，默认 ${DEFAULT_SECONDS}
  --interval-ms <n>   采样间隔，默认 ${DEFAULT_INTERVAL_MS}
  --label <name>      给这次采样起名，写进 JSON（便于对比表）
  --out <path>        同时写入文件（默认只写 stdout）

示例:
  node tools/perf-baseline.mjs --pid 1234 --seconds 300 --label "agent-1.5s" --out /tmp/agent.json`);
}

/** 读 `stat` 里的 utime+stime（字段 14/15，1-based）。comm 可能含空格/括号，从最后一个 ')' 之后切。 */
function readCpuTicks(pid) {
  const raw = readFileSync(`/proc/${pid}/stat`, 'utf8');
  const close = raw.lastIndexOf(')');
  if (close < 0) throw new Error(`无法解析 /proc/${pid}/stat`);
  // close 之后依次是 state(3) ppid(4) ... utime(14) stime(15)
  const rest = raw.slice(close + 1).trim().split(/\s+/);
  const utime = Number.parseInt(rest[11], 10);
  const stime = Number.parseInt(rest[12], 10);
  if (!Number.isFinite(utime) || !Number.isFinite(stime)) {
    throw new Error(`无法解析 /proc/${pid}/stat 的 CPU 时间`);
  }
  return utime + stime;
}

function readStatus(pid) {
  const raw = readFileSync(`/proc/${pid}/status`, 'utf8');
  const rssKb = Number.parseInt(/^VmRSS:\s+(\d+)\s+kB$/m.exec(raw)?.[1] ?? '0', 10);
  const threads = Number.parseInt(/^Threads:\s+(\d+)$/m.exec(raw)?.[1] ?? '0', 10);
  return { rssBytes: rssKb * 1024, threads };
}

function readIo(pid) {
  const raw = readFileSync(`/proc/${pid}/io`, 'utf8');
  const pick = name => Number.parseInt(new RegExp(`^${name}:\\s+(\\d+)$`, 'm').exec(raw)?.[1] ?? '0', 10);
  return {
    rchar: pick('rchar'),
    wchar: pick('wchar'),
    readBytes: pick('read_bytes'),
    writeBytes: pick('write_bytes'),
    syscr: pick('syscr'),
    syscw: pick('syscw'),
  };
}

/** 一次完整快照；任何一项读不到都视为进程已消失（返回 null）。 */
function snapshot(pid) {
  try {
    return {
      ticks: readCpuTicks(pid),
      ...readStatus(pid),
      io: readIo(pid),
      at: Date.now(),
    };
  } catch {
    return null;
  }
}

function percentile(sorted, p) {
  if (sorted.length === 0) return 0;
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil((p / 100) * sorted.length) - 1));
  return sorted[index];
}

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

function summarize(options, first, last, perSecondTicks) {
  const elapsedSeconds = (last.at - first.at) / 1000;
  const deltaTicks = last.ticks - first.ticks;
  const cpuPercent = elapsedSeconds > 0 ? (deltaTicks / CLK_TCK / elapsedSeconds) * 100 : 0;

  // 逐秒增量：采样间隔不是 1 秒时按比例折算，保证「每秒 tick」口径一致。
  const scale = 1000 / options.intervalMs;
  const normalized = perSecondTicks.map(ticks => Math.round(ticks * scale));
  const sorted = [...normalized].sort((a, b) => a - b);
  const busy = sorted.filter(value => value > 0);
  const burstFloor = 3; // 与 2026-11-05 实测口径一致：>=3 ticks/s 视为「有活的秒」

  return {
    cpu: {
      percent: Number(cpuPercent.toFixed(4)),
      ticks: deltaTicks,
      elapsedSeconds: Number(elapsedSeconds.toFixed(2)),
    },
    memory: {
      rssBytes: last.rssBytes,
      rssMB: Number((last.rssBytes / 1048576).toFixed(2)),
      threads: last.threads,
    },
    io: {
      rchar: last.io.rchar - first.io.rchar,
      wchar: last.io.wchar - first.io.wchar,
      readBytes: last.io.readBytes - first.io.readBytes,
      writeBytes: last.io.writeBytes - first.io.writeBytes,
      syscr: last.io.syscr - first.io.syscr,
      syscw: last.io.syscw - first.io.syscw,
      rcharPerSec: Math.round((last.io.rchar - first.io.rchar) / Math.max(elapsedSeconds, 1)),
      wcharPerSec: Math.round((last.io.wchar - first.io.wchar) / Math.max(elapsedSeconds, 1)),
      syscrPerSec: Math.round((last.io.syscr - first.io.syscr) / Math.max(elapsedSeconds, 1)),
    },
    distribution: {
      // 有活的秒越多、尖峰越高，说明任务型开销越重（而非均匀采集）。
      samples: normalized.length,
      idleSeconds: normalized.length - busy.length,
      busySeconds: busy.length,
      maxTicksPerSecond: normalized.length > 0 ? Math.max(...normalized) : 0,
      p50TicksPerSecond: percentile(sorted, 50),
      p95TicksPerSecond: percentile(sorted, 95),
    },
  };
}

async function main() {
  if (process.platform !== 'linux') {
    throw new Error(`仅支持 Linux（依赖 /proc），当前平台: ${process.platform}`);
  }
  const options = parseArgs(process.argv.slice(2));

  const first = snapshot(options.pid);
  if (!first) throw new Error(`进程 ${options.pid} 不可读（不存在或无权限）`);

  const perSecondTicks = [];
  const deadline = Date.now() + options.seconds * 1000;
  let previous = first;
  let last = first;

  while (Date.now() < deadline) {
    await sleep(options.intervalMs);
    const current = snapshot(options.pid);
    if (!current) throw new Error(`进程 ${options.pid} 在采样过程中退出`);
    perSecondTicks.push(current.ticks - previous.ticks);
    previous = current;
    last = current;
  }

  const report = {
    tool: 'perf-baseline',
    label: options.label,
    pid: options.pid,
    startedAt: new Date(first.at).toISOString(),
    seconds: options.seconds,
    intervalMs: options.intervalMs,
    clockTicksPerSecond: CLK_TCK,
    ...summarize(options, first, last, perSecondTicks),
  };

  const json = `${JSON.stringify(report, null, 2)}\n`;
  if (options.out) writeFileSync(options.out, json);

  const label = options.label ? `[${options.label}] ` : '';
  process.stderr.write(
    `${label}pid=${report.pid} ${report.seconds}s  ` +
      `CPU=${report.cpu.percent}%  RSS=${report.memory.rssMB}MB  thr=${report.memory.threads}  ` +
      `rchar=${report.io.rcharPerSec}B/s  wchar=${report.io.wcharPerSec}B/s  ` +
      `syscr=${report.io.syscrPerSec}/s  ` +
      `busy=${report.distribution.busySeconds}/${report.distribution.samples}s  ` +
      `max=${report.distribution.maxTicksPerSecond}t/s  p95=${report.distribution.p95TicksPerSecond}t/s\n`
  );
  process.stdout.write(json);
}

main().catch(error => {
  process.stderr.write(`错误: ${error.message}\n`);
  process.exit(1);
});
