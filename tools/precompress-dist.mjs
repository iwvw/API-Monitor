#!/usr/bin/env node
/**
 * 构建期静态资源预压缩（gzip + brotli）。
 *
 * 为什么存在
 * ----------
 * 运行期实时 gzip 让静态资源成为 CPU 热点：486KB 的 CSS 从 50 并发就打满 2 核，
 * 只有 3782 RPS。把压缩成本一次性挪到构建期，运行期直接发送 `.gz` / `.br`，
 * 请求路径上就没有压缩计算了。
 *
 * 约定
 * ----
 *  - 只压文本类型；已压缩过的二进制（woff2/png/jpg/zip/mp4…）再压只会变大。
 *  - 跳过 `dist/agent/`：那是 Agent 二进制与 .sha256 下载产物，不能被替换。
 *  - 小于 1KB 不压：gzip 头尾约 20B 开销，压完可能更大。
 *  - 生成的 `.gz` / `.br` 必须继承源文件 mtime，否则 `If-Modified-Since` 会失真。
 *  - 只写磁盘，不改动源文件；重复执行会覆盖旧产物。
 *
 * 运行期对应实现见 backend-go/internal/server/static_precompressed.go，
 * 两边的扩展名白名单必须保持一致。
 *
 * 用法：
 *   node tools/precompress-dist.mjs [distDir]
 */

import { existsSync, readdirSync, readFileSync, statSync, utimesSync, writeFileSync } from 'node:fs';
import { join, extname, relative } from 'node:path';
import { gzipSync, brotliCompressSync, constants as zlibConstants } from 'node:zlib';

// 必须与 backend-go/internal/server/static_precompressed.go 的
// precompressedExtensions 保持一致：Go 侧只对这些扩展名尝试查找预压缩产物。
const COMPRESSIBLE_EXTENSIONS = new Set([
  '.js', '.mjs', '.css', '.html', '.svg', '.json',
  '.webmanifest', '.map', '.txt', '.xml', '.wasm',
]);

// 构建期一次性成本，换最小体积。
const GZIP_LEVEL = 9;
const BROTLI_QUALITY = 11; // 0-11，11 最慢但体积最小
const BROTLI_LGWIN = 24;   // 16MB window
const MIN_SIZE_BYTES = 1024;

// 只跳过 dist 根下的一级目录：`agent/` 是二进制下载产物，压了反而破坏校验。
const SKIP_ROOT_DIRS = new Set(['agent']);

const distDir = process.argv[2] || 'dist';

if (!existsSync(distDir) || !statSync(distDir).isDirectory()) {
  console.error(`precompress: dist directory not found: ${distDir}`);
  process.exit(1);
}

const stats = { processed: 0, skippedSmall: 0, skippedType: 0, failures: 0 };
let rawBytes = 0;
let gzipBytes = 0;
let brotliBytes = 0;

function compressFile(file, size) {
  const source = readFileSync(file);
  const gz = gzipSync(source, { level: GZIP_LEVEL });
  const br = brotliCompressSync(source, {
    params: {
      [zlibConstants.BROTLI_PARAM_QUALITY]: BROTLI_QUALITY,
      [zlibConstants.BROTLI_PARAM_LGWIN]: BROTLI_LGWIN,
      [zlibConstants.BROTLI_PARAM_SIZE_HINT]: size,
    },
  });
  writeFileSync(`${file}.gz`, gz);
  writeFileSync(`${file}.br`, br);

  // 继承源文件时间戳，保证 If-Modified-Since / Last-Modified 与源文件一致。
  const stat = statSync(file);
  utimesSync(`${file}.gz`, stat.atime, stat.mtime);
  utimesSync(`${file}.br`, stat.atime, stat.mtime);

  stats.processed += 1;
  rawBytes += size;
  gzipBytes += gz.length;
  brotliBytes += br.length;
}

function walk(dir, isRoot) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (isRoot && SKIP_ROOT_DIRS.has(entry.name)) continue;
      walk(full, false);
      continue;
    }
    if (!entry.isFile()) continue;
    if (!COMPRESSIBLE_EXTENSIONS.has(extname(entry.name).toLowerCase())) {
      stats.skippedType += 1;
      continue;
    }
    try {
      const stat = statSync(full);
      if (stat.size < MIN_SIZE_BYTES) {
        stats.skippedSmall += 1;
        continue;
      }
      compressFile(full, stat.size);
    } catch (err) {
      stats.failures += 1;
      console.error(`precompress: failed ${relative(distDir, full)}: ${err.message}`);
    }
  }
}

walk(distDir, true);

const mb = n => `${(n / 1024 / 1024).toFixed(2)}MB`;
const ratio = (n) => (rawBytes > 0 ? `${((n / rawBytes) * 100).toFixed(1)}%` : '0%');

console.log(
  `precompress: ${stats.processed} files compressed, ` +
  `${stats.skippedSmall} skipped (<${MIN_SIZE_BYTES}B), ` +
  `${stats.skippedType} skipped (non-text).`,
);
console.log(
  `precompress: raw ${mb(rawBytes)} → gzip ${mb(gzipBytes)} (${ratio(gzipBytes)}) ` +
  `| brotli ${mb(brotliBytes)} (${ratio(brotliBytes)})`,
);

if (stats.failures > 0) {
  console.error(`precompress: ${stats.failures} failure(s)`);
  process.exit(1);
}
