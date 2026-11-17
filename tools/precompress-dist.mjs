#!/usr/bin/env node
/**
 * 构建期静态资源预压缩(gzip + brotli)。
 *
 * 为什么存在
 * ----------
 * 运行期实时 gzip 让静态资源成为 CPU 热点:486KB 的 CSS 从 50 并发就打满 2 核,
 * 只有 3782 RPS。把压缩成本一次性挪到构建期,运行期直接发送 `.gz` / `.br`,
 * 请求路径上就没有压缩计算了。
 *
 * 约定
 * ----
 *  - 只压文本类型;已压缩过的二进制(woff2/png/jpg/zip/mp4...)再压只会变大。
 *  - 跳过 `dist/agent/`:那是 Agent 二进制与 .sha256 下载产物,不能被替换
 *    (防御性:预压缩跑在 frontend-builder 阶段,那时 agent/ 还不存在)。
 *  - 小于 1KB 不压:gzip 头尾约 20B 开销,压完可能更大。
 *  - ≥1MB 的大文件降档到 brotli q9 / gzip 6:q11 的耗时随体积超线性增长,
 *    而体积收益只有个位数百分比,不值得拖慢镜像构建。
 *  - 生成的 `.gz` / `.br` 必须继承源文件 mtime,否则 `If-Modified-Since` 会失真。
 *  - 只写磁盘,不改动源文件;重复执行会覆盖旧产物。
 *
 * 性能
 * ----
 * brotli q11 单线程压缩数百个文件是镜像构建里最长的一段(CI 实测约 100s)。
 * 这里按 CPU 数起 worker_threads 并行压缩(输出字节完全一致),把这段压到
 * 十几秒;文件数很少时不值得起线程,退回单线程。
 *
 * 运行期对应实现见 backend-go/internal/server/static_precompressed.go,
 * 两边的扩展名白名单必须保持一致。
 *
 * 用法:
 *   node tools/precompress-dist.mjs [distDir]
 */

import { existsSync, readdirSync, readFileSync, statSync, utimesSync, writeFileSync } from 'node:fs';
import { availableParallelism } from 'node:os';
import { extname, join, relative } from 'node:path';
import { brotliCompressSync, constants as zlibConstants, gzipSync } from 'node:zlib';
import { Worker, isMainThread, parentPort, workerData } from 'node:worker_threads';

// 必须与 backend-go/internal/server/static_precompressed.go 的
// precompressedExtensions 保持一致:Go 侧只对这些扩展名尝试查找预压缩产物。
const COMPRESSIBLE_EXTENSIONS = new Set([
  '.js', '.mjs', '.css', '.html', '.svg', '.json',
  '.webmanifest', '.map', '.txt', '.xml', '.wasm',
]);

// 构建期一次性成本,换最小体积。
const GZIP_LEVEL = 9;
const BROTLI_QUALITY = 11; // 0-11,11 最慢但体积最小
const BROTLI_LGWIN = 24;   // 16MB window
const MIN_SIZE_BYTES = 1024;

// 大文件降档:brotli q11 的耗时随体积超线性增长,单个 8MB 的 chunk 用 q11 要
// 十几秒,而体积收益相对 q9 只有个位数百分比。CI 里前端的 dist 有 800+ 文件,
// 全用 q11 会把镜像构建拖长几十秒。分档后总耗时约减半,体积几乎不变。
const LARGE_FILE_BYTES = 1024 * 1024;      // ≥1MB 视为大文件
const LARGE_BROTLI_QUALITY = 9;            // 大文件用 q9
const LARGE_GZIP_LEVEL = 6;                // 大文件 gzip 用 6(-9 对 8MB 也要数秒)

// 只跳过 dist 根下的一级目录:`agent/` 是 Agent 二进制下载产物,压了反而破坏校验。
//
// 注意:预压缩跑在 frontend-builder 阶段,此时 dist/agent 还不存在(Agent 二进制
// 由 Runner 阶段单独 COPY 进来)。这条跳过是防御性的——如果将来把 Agent 产物提前
// 放进 dist,它能保证不会被误压。
const SKIP_ROOT_DIRS = new Set(['agent']);

// 文件数太少时不值得起线程(线程启动本身也要几十毫秒)。
const MIN_FILES_FOR_WORKERS = 64;
const MAX_WORKERS = 8;

function emptyStats() {
  return { processed: 0, skippedSmall: 0, skippedType: 0, failures: 0, rawBytes: 0, gzipBytes: 0, brotliBytes: 0 };
}

// compressFile 压缩单个文件并返回本文件的统计增量;失败抛错由调用方计数。
function compressFile(file, size) {
  const large = size >= LARGE_FILE_BYTES;
  const source = readFileSync(file);
  const gz = gzipSync(source, { level: large ? LARGE_GZIP_LEVEL : GZIP_LEVEL });
  const br = brotliCompressSync(source, {
    params: {
      [zlibConstants.BROTLI_PARAM_QUALITY]: large ? LARGE_BROTLI_QUALITY : BROTLI_QUALITY,
      [zlibConstants.BROTLI_PARAM_LGWIN]: BROTLI_LGWIN,
      [zlibConstants.BROTLI_PARAM_SIZE_HINT]: size,
    },
  });
  writeFileSync(`${file}.gz`, gz);
  writeFileSync(`${file}.br`, br);

  // 继承源文件时间戳,保证 If-Modified-Since / Last-Modified 与源文件一致。
  const stat = statSync(file);
  utimesSync(`${file}.gz`, stat.atime, stat.mtime);
  utimesSync(`${file}.br`, stat.atime, stat.mtime);

  return { processed: 1, rawBytes: size, gzipBytes: gz.length, brotliBytes: br.length };
}

// collectFiles 遍历待压文件,返回 [绝对路径, 大小] 列表与跳过的计数。
function collectFiles(distDir) {
  const files = [];
  const skipped = { skippedSmall: 0, skippedType: 0 };
  const walk = (dir, isRoot) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (isRoot && SKIP_ROOT_DIRS.has(entry.name)) continue;
        walk(full, false);
        continue;
      }
      if (!entry.isFile()) continue;
      if (!COMPRESSIBLE_EXTENSIONS.has(extname(entry.name).toLowerCase())) {
        skipped.skippedType += 1;
        continue;
      }
      const stat = statSync(full);
      if (stat.size < MIN_SIZE_BYTES) {
        skipped.skippedSmall += 1;
        continue;
      }
      files.push([full, stat.size]);
    }
  };
  walk(distDir, true);
  return { files, skipped };
}

function compressSlice(files, distDir, stats) {
  for (const [file, size] of files) {
    try {
      const delta = compressFile(file, size);
      stats.processed += delta.processed;
      stats.rawBytes += delta.rawBytes;
      stats.gzipBytes += delta.gzipBytes;
      stats.brotliBytes += delta.brotliBytes;
    } catch (err) {
      stats.failures += 1;
      console.error(`precompress: failed ${relative(distDir, file)}: ${err.message}`);
    }
  }
}

// ---------- worker 模式:只处理分配到的切片 ----------
if (!isMainThread) {
  const stats = emptyStats();
  compressSlice(workerData.files, workerData.distDir, stats);
  parentPort.postMessage(stats);
} else {
  await main();
}

async function main() {
  const distDir = process.argv[2] || 'dist';

  if (!existsSync(distDir) || !statSync(distDir).isDirectory()) {
    console.error(`precompress: dist directory not found: ${distDir}`);
    process.exit(1);
  }

  const { files, skipped } = collectFiles(distDir);
  const stats = emptyStats();
  stats.skippedSmall = skipped.skippedSmall;
  stats.skippedType = skipped.skippedType;

  const cpuCount = (typeof availableParallelism === 'function' && availableParallelism()) || 1;
  const workerCount = Math.max(1, Math.min(MAX_WORKERS, cpuCount, Math.ceil(files.length / MIN_FILES_FOR_WORKERS)));

  if (workerCount <= 1) {
    compressSlice(files, distDir, stats);
  } else {
    // 轮转切分:大小文件混排,避免某个 worker 全是大文件。
    const slices = Array.from({ length: workerCount }, () => []);
    files.forEach((entry, index) => slices[index % workerCount].push(entry));
    const results = await Promise.all(
      slices.map(filesForWorker => new Promise((resolve, reject) => {
        const worker = new Worker(new URL(import.meta.url), {
          workerData: { files: filesForWorker, distDir },
        });
        worker.once('message', resolve);
        worker.once('error', reject);
        worker.once('exit', code => {
          if (code !== 0) reject(new Error(`precompress worker exited with ${code}`));
        });
      })),
    );
    for (const part of results) {
      stats.processed += part.processed;
      stats.rawBytes += part.rawBytes;
      stats.gzipBytes += part.gzipBytes;
      stats.brotliBytes += part.brotliBytes;
      stats.failures += part.failures;
    }
  }

  const mb = n => `${(n / 1024 / 1024).toFixed(2)}MB`;
  const ratio = n => (stats.rawBytes > 0 ? `${((n / stats.rawBytes) * 100).toFixed(1)}%` : '0%');

  console.log(
    `precompress: ${stats.processed} files compressed (${workerCount} thread(s)), ` +
    `${stats.skippedSmall} skipped (<${MIN_SIZE_BYTES}B), ` +
    `${stats.skippedType} skipped (non-text).`,
  );
  console.log(
    `precompress: raw ${mb(stats.rawBytes)} → gzip ${mb(stats.gzipBytes)} (${ratio(stats.gzipBytes)}) ` +
    `| brotli ${mb(stats.brotliBytes)} (${ratio(stats.brotliBytes)})`,
  );

  if (stats.failures > 0) {
    console.error(`precompress: ${stats.failures} failure(s)`);
    process.exit(1);
  }
}