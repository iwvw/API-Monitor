#!/usr/bin/env node
// API-Sweep：对运行中的 Go 后端做全量接口连通性/可用性巡检。
//
// 用法：
//   node tools/api-full-sweep.mjs [--base <url>] [--password <admin-password>] [--limit <n>] [--readonly]
//
// 流程：
//   1. POST /api/auth/login 获取 session（sid cookie）。
//   2. GET /api/system/openapi.json 取得路由清单（带路径参数示例）。
//   3. 并发探测：优先只读 GET；标记为写操作的路径默认跳过破坏性动作，
//      仅用「不存在的资源 ID + 空 body」验证非 5xx（证明路由已 dispatch 而非丢失）。
//   4. 按状态码分桶输出 JSON 报告。
//
// 仅用于本地/自控环境；生产环境请加 --readonly 并确认不影响数据。

import fs from 'node:fs';
import process from 'node:process';
import path from 'node:path';

const args = {};
for (let i = 2; i < process.argv.length; i++) {
  const a = process.argv[i];
  if (a.startsWith('--')) {
    const key = a.slice(2);
    const val = process.argv[i + 1];
    if (val !== undefined && !val.startsWith('--')) {
      args[key] = val;
      i++;
    } else {
      args[key] = true;
    }
  }
}

const base =
  process.env.API_MONITOR_BASE_URL || args.base || 'http://127.0.0.1:3000';
const password = args.password;
const limit = args.limit ? parseInt(args.limit, 10) : 0;
const readonly = !!args.readonly;
const concurrency = args.concurrency ? parseInt(args.concurrency, 10) : 8;
const timeoutMs = args.timeout ? parseInt(args.timeout, 10) : 8000;

// 破坏性/高副作用接口：写探测一律跳过，仅验证其存在（manifest 命中）。
const destructiveWritePrefixes = [
  '/api/settings/vacuum-database',
  '/api/settings/cleanup-deprecated-tables',
  '/api/settings/clear-logs',
  '/api/settings/clear-app-logs',
  '/api/settings/database/import/commit',
  '/api/settings/database/import',
  '/api/settings/export-database',
  '/api/backup/restore',
  '/api/backup/run',
  '/api/auth/change-password',
  '/api/auth/set-password',
];

// 会破坏当前登录会话或修改核心凭据的接口：一律跳过探测，仅记录。
const sessionDestroyingPrefixes = [
  '/api/auth/logout',
  '/api/auth/set-password',
  '/api/auth/change-password',
  '/api/auth/2fa/disable',
  '/api/auth/webauthn/credentials',
  '/api/auth/webauthn/register',
];

class Sweeper {
  constructor() {
    this.cookie = '';
    this.results = [];
    this.buckets = new Map();
  }

  async request(method, urlPath, { body, headers = {}, truncate = true } = {}) {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), timeoutMs);
    const init = { method, headers: {
      ...headers,
      ...(this.cookie ? { Cookie: this.cookie } : {}),
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    }, signal: ctl.signal };
    if (body !== undefined) init.body = JSON.stringify(body);
    const started = Date.now();
    try {
      const res = await fetch(base + urlPath, init);
      const text = await res.text();
      return { status: res.status, body: truncate ? text.slice(0, 300) : text, ms: Date.now() - started };
    } catch (err) {
      return { status: 0, body: String(err?.message || err), ms: Date.now() - started };
    } finally {
      clearTimeout(timer);
    }
  }

  async login() {
    const res = await fetch(base + '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password }),
    });
    const setCookieRaw = res.headers.get('set-cookie') || '';
    const text = await res.text();
    const first = setCookieRaw.split(',')[0].trim();
    if (first.startsWith('sid=')) {
      this.cookie = first.split(';')[0];
    }
    if (!this.cookie && res.status === 200) {
      // 登录成功但未下发 sid（如 demo 模式）——后续接口会返回 401，属预期。
      return true;
    }
    if (!this.cookie && res.status !== 200) {
      throw new Error(`登录失败 (${res.status}): ${text.slice(0, 200)}`);
    }
    return true;
  }

  async fetchOpenAPI() {
    const res = await this.request('GET', '/api/system/openapi.json', { truncate: false });
    if (res.status !== 200) {
      throw new Error(`OpenAPI 获取失败 (${res.status}): ${res.body}`);
    }
    return JSON.parse(res.body);
  }

  bucket(status, isWrite, body = '') {
    if (typeof status !== 'number') return status;
    if (status === 0) return isWrite ? 'netwrite' : 'network_err';
    if (status >= 200 && status < 300) return 'ok';
    if (status === 401) return 'auth_fail';
    if (status === 403) return 'forbidden';
    // 用响应体语义区分「路由不存在/未实现」与「路由存在但资源/参数异常」。
    const routeMissing =
      /route not implemented|v1 endpoint not found|管理 AI 路由不存在|openai admin route not found|agent route not found|monitor sub-route not found|remote desktop route not found|v2 docker route not found|Docker operation not found/i.test(body);
    if (status === 404) {
      if (routeMissing) return 'not_implemented';
      return isWrite ? 'resource_missing_write' : 'resource_missing';
    }
    if (status === 405) return 'method_ok';
    if (status === 429) return 'rate_limited';
    if (status >= 400 && status < 500) return 'client_err'; // 参数校验类：路由存在
    if (status >= 500) return 'server_err';
    return 'other';
  }

  async run() {
    console.log(`[sweep] base=${base} readonly=${readonly} password=${password ? 'provided' : 'MISSING'}`);
    if (!password) {
      throw new Error('--password 必填（管理员密码）');
    }
    await this.login();
    console.log('[sweep] logged in, sid cookie acquired');
    const doc = await this.fetchOpenAPI();
    const paths = Object.keys(doc.paths || {});
    console.log(`[sweep] openapi paths=${paths.length}`);
    const targets = [];
    for (const p of paths) {
      const ops = doc.paths[p];
      const methods = Object.keys(ops).filter((m) => ['get', 'post', 'put', 'patch', 'delete', 'head'].includes(m));
      targets.push({ path: p, methods, raw: ops });
    }
    const slice = limit ? targets.slice(0, limit) : targets;
    console.log(`[sweep] probing ${slice.length} targets (concurrency=${concurrency})...`);

    let cursor = 0;
    const workers = Array.from({ length: Math.min(concurrency, slice.length) }, async () => {
      while (cursor < slice.length) {
        const item = slice[cursor++];
        const { path, methods, raw } = item;
        // 破坏性/会话破坏接口：跳过实际探测，仅记录。
        if (destructiveWritePrefixes.some((d) => path.startsWith(d))) {
          this.results.push({ path, method: 'skip', status: 'skipped_destructive', body: '', ms: 0 });
          continue;
        }
        if (sessionDestroyingPrefixes.some((d) => path.startsWith(d))) {
          this.results.push({ path, method: 'skip', status: 'skipped_session_destroying', body: '', ms: 0 });
          continue;
        }
        const filled = this.fillPathParams(path, raw);
        // 方法探测序列：GET → HEAD → POST → PUT → PATCH → DELETE（空体）。
        const order = readonly ? ['GET', 'HEAD'] : ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'];
        if (readonly && !order.some((m) => methods.includes(m.toLowerCase()))) {
          this.results.push({ path, method: 'skip', status: 'skipped_readonly_write', body: '', ms: 0 });
          continue;
        }
        const tried = [];
        let result = null;
        for (const method of order) {
          if (!methods.includes(method.toLowerCase())) continue;
          const body = ['POST', 'PUT', 'PATCH'].includes(method) ? {} : undefined;
          const res = await this.request(method, filled, { body });
          tried.push({ method, status: res.status, body: res.body });
          if (this.isRoutePresent(res.status, res.body)) {
            result = res;
            result.method = method;
            break;
          }
        }
        if (!result) {
          // 所有方法都"路由缺失"：取最后一次响应用于分类（404/405/网络等）。
          const last = tried[tried.length - 1];
          result = { ...(last || {}), method: last?.method || (methods[0] || 'GET'), tried: tried.map((t) => `${t.method}:${t.status}`) };
        } else {
          result.tried = tried.map((t) => `${t.method}:${t.status}`);
        }
        // readonly 下只探测了 GET/HEAD：若唯一尝试是 GET 且返回路由层 404，
        // 无法确认"路由缺失"还是"仅写接口无 GET"——单独标记供人工复核。
        if (readonly && tried.length === 1 && tried[0].method === 'GET' && !this.isRoutePresent(tried[0].status, tried[0].body) && tried[0].status === 404) {
          this.results.push({ path, method: 'GET', status: 'readonly_maybe_write_only', body: result.body, ms: result.ms, tried: tried.map((t) => `${t.method}:${t.status}`) });
          continue;
        }
        this.results.push({ path, method: result.method, status: result.status, body: result.body, ms: result.ms, tried: result.tried });
      }
    });
    await Promise.all(workers);

    // 分桶统计
    for (const r of this.results) {
      const b = typeof r.status === 'number' ? this.bucket(r.status, this.isWriteMethod(r.method), r.body) : r.status;
      r.bucket = b;
      if (!this.buckets.has(b)) this.buckets.set(b, []);
      this.buckets.get(b).push(r);
    }
  }

  isWriteMethod(method) {
    return typeof method === 'string' && ['post', 'put', 'patch', 'delete'].includes(method.toLowerCase());
  }

  isRoutePresent(status, body = '') {
    if (typeof status !== 'number' || status === 0) return false;
    if (status >= 200 && status < 300) return true;
    if (status === 401 || status === 403 || status === 405 || status === 429) return true;
    if (status >= 500) return false; // 5xx 视为存在但异常（归入 server_err，另行复核）
    if (status === 404) {
      return !/route not implemented|v1 endpoint not found|管理 AI 路由不存在|openai admin route not found|agent route not found|monitor sub-route not found|remote desktop route not found|v2 docker route not found|Docker operation not found/i.test(body);
    }
    return true; // 其余 4xx 参数校验 = 路由存在
  }

  fillPathParams(p, op) {
    if (!p.includes('{')) return p;
    const params = (op?.parameters || []).filter((x) => x.in === 'path');
    let out = p;
    for (const m of p.matchAll(/\{([^}]+)\}/g)) {
      const name = m[1];
      const def = params.find((x) => x.name === name);
      const example = def?.schema?.example ?? def?.example ?? '1';
      out = out.replace(m[0], encodeURIComponent(String(example)));
    }
    return out.replace(/\{([^}]+)\}/g, '1');
  }

  report(outFile) {
    const summary = {};
    for (const [b, list] of this.buckets) summary[b] = list.length;
    const report = {
      base,
      readonly,
      generatedAt: new Date().toISOString(),
      total: this.results.length,
      summary,
      buckets: Object.fromEntries([...this.buckets.entries()].map(([k, v]) => [k, v.map((r) => ({ path: r.path, method: r.method, status: r.status, ms: r.ms, body: r.body, tried: r.tried }))])),
    };
    if (outFile) {
      fs.writeFileSync(outFile, JSON.stringify(report, null, 2), 'utf8');
    }
    return report;
  }
}

const sweep = new Sweeper();
sweep
  .run()
  .then(() => {
    const outPath = args.out
      ? path.resolve(args.out)
      : path.resolve('backend-go', 'tmp', `api-sweep-${Date.now()}.json`);
    if (!args.out) {
      fs.mkdirSync(path.dirname(outPath), { recursive: true });
    }
    const report = sweep.report(outPath);
    console.log(`[sweep] done. total=${report.total}`);
    console.log('[sweep] bucket summary:', JSON.stringify(report.summary, null, 2));
    if (report.buckets.not_implemented?.length) {
      console.error(`[sweep] ${report.buckets.not_implemented.length} not_implemented —— 路由缺失/未实现`);
    }
    if (report.buckets.server_err?.length) {
      console.error(`[sweep] ${report.buckets.server_err.length} server_error —— 需要人工复核`);
    }
    if (report.buckets.resource_missing?.length) {
      console.error(`[sweep] ${report.buckets.resource_missing.length} resource_missing —— 可能路由未 dispatch 或资源不存在`);
    }
    if (report.buckets.resource_missing_write?.length) {
      console.error(`[sweep] ${report.buckets.resource_missing_write.length} 写接口探测返回 404 —— 确认路由是否注册`);
    }
    console.log(`[sweep] report saved to ${outPath}`);
  })
  .catch((err) => {
    console.error(`[sweep] FAILED: ${err.message}`);
    process.exit(1);
  });