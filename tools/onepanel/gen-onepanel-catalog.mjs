#!/usr/bin/env node
/*
 * gen-onepanel-catalog.mjs — 1Panel 快捷控制模块的单一事实源生成器。
 *
 * 输入：
 *   tools/onepanel/quick-routes.json        人工维护的「对外路径 → 1Panel 上游」映射
 *   1Panel 官方 swagger（core/cmd/server/docs/docs.go，swag 生成）
 *
 * 输出（全部由本脚本覆盖，禁止手改）：
 *   backend-go/internal/onepanel/catalog.json   通用代理用的官方端点目录
 *   backend-go/internal/onepanel/routes_gen.go  快捷路由表（含上游方法/默认请求体/查询参数）
 *   backend-go/internal/system/onepanel_gen.go  文档 seed、请求契约、方法推断表、中文描述
 *
 * 用法：
 *   node tools/onepanel/gen-onepanel-catalog.mjs                 # 拉取 quick-routes 里钉住的 tag
 *   node tools/onepanel/gen-onepanel-catalog.mjs --tag v2.4.1    # 指定 tag
 *   node tools/onepanel/gen-onepanel-catalog.mjs --swagger-file /tmp/swagger.json
 *   node tools/onepanel/gen-onepanel-catalog.mjs --check         # 只校验不写文件（CI 用）
 *
 * 校验项（任一失败即退出码 1，不写任何文件）：
 *   - quick-routes 里每个 upstream 的 method/path 都能在 swagger 中找到
 *   - upstream 引用的 query 参数在 swagger 中真实存在
 *   - search 类默认请求体覆盖 swagger 声明的全部 required 字段
 *   - prefix 里的 {param} 都被 upstream 的 path/bodyFrom/query 至少引用一次
 */
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = path.resolve(import.meta.dirname, '../..');
const routesFile = path.join(root, 'tools/onepanel/quick-routes.json');
const catalogOut = path.join(root, 'backend-go/internal/onepanel/catalog.json');
const routesGoOut = path.join(root, 'backend-go/internal/onepanel/routes_gen.go');
const systemGoOut = path.join(root, 'backend-go/internal/system/onepanel_gen.go');
const cacheRoot = path.join(root, '.cache/onepanel');
const snapshotDir = path.join(root, 'tools/onepanel/swagger');

const args = process.argv.slice(2);
const checkOnly = args.includes('--check');
const pinMode = args.includes('--pin');
const argValue = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};

const spec = JSON.parse(fs.readFileSync(routesFile, 'utf8'));
const swaggerTag = argValue('--tag') || spec.swaggerVersion;
const swaggerFileArg = argValue('--swagger-file');

const errors = [];
const fail = (msg) => errors.push(msg);

// ---------- 1. 取官方 swagger ----------

/**
 * 加载官方 swagger。
 * 优先级：--swagger-file > 仓库内快照 tools/onepanel/swagger/<tag>.json > 在线拉取并写缓存。
 * 快照入库是为了让 CI 完全离线、并且把「对齐的是哪一版 1Panel」变成可 review 的事实。
 */
async function loadSwagger() {
  if (swaggerFileArg) {
    return { doc: JSON.parse(fs.readFileSync(swaggerFileArg, 'utf8')), source: swaggerFileArg };
  }
  const pinnedFile = path.join(snapshotDir, `${swaggerTag}.json`);
  if (fs.existsSync(pinnedFile)) {
    return { doc: JSON.parse(fs.readFileSync(pinnedFile, 'utf8')), source: `${path.relative(root, pinnedFile)} (pinned)` };
  }
  const cacheFile = path.join(cacheRoot, `${swaggerTag}.json`);
  if (fs.existsSync(cacheFile)) {
    return { doc: JSON.parse(fs.readFileSync(cacheFile, 'utf8')), source: `${cacheFile} (cache)` };
  }
  const url = `https://raw.githubusercontent.com/1Panel-dev/1Panel/${swaggerTag}/core/cmd/server/docs/docs.go`;
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(
      `没有 ${swaggerTag} 的 swagger 快照，在线拉取也失败: ${url} → HTTP ${res.status}\n` +
        `离线环境请先执行：node tools/onepanel/gen-onepanel-catalog.mjs --tag ${swaggerTag} --pin`,
    );
  }
  const src = await res.text();
  const start = src.indexOf('const docTemplate = `');
  if (start < 0) throw new Error('docs.go 中找不到 docTemplate');
  const bodyStart = start + 'const docTemplate = `'.length;
  const end = src.indexOf('`\n\nvar SwaggerInfo', bodyStart);
  if (end < 0) throw new Error('docs.go 中找不到 docTemplate 结束标记');
  // Go 原始字符串里嵌入反引号会写成 ` + "`" + `
  const raw = src.slice(bodyStart, end).replace(/` \+ "`" \+ `/g, '`');
  const doc = JSON.parse(raw);
  fs.mkdirSync(cacheRoot, { recursive: true });
  fs.writeFileSync(cacheFile, JSON.stringify(doc));
  return { doc, source: `${url} (fetched, cached)` };
}

// ---------- 2. 工具函数 ----------

const HTTP_METHODS = ['get', 'post', 'put', 'delete', 'patch'];

/** swagger 路径参数有 `:name` 与 `{name}` 两种写法，统一成 `{name}` 便于比对。 */
function normalizePath(p) {
  return p.replace(/:([A-Za-z0-9_]+)/g, '{$1}');
}

function swaggerOps(doc) {
  const ops = new Map(); // normalizedPath -> Map(method -> operation)
  for (const [rawPath, methods] of Object.entries(doc.paths || {})) {
    const key = normalizePath(rawPath);
    const m = new Map();
    for (const [method, op] of Object.entries(methods)) {
      if (HTTP_METHODS.includes(method)) m.set(method.toUpperCase(), op);
    }
    ops.set(key, m);
  }
  return ops;
}

function deref(doc, ref) {
  if (!ref) return null;
  const name = ref.split('/').pop();
  return doc.definitions?.[name] || null;
}

/** 把一个 operation 的请求体（body 参数）解析成 { name, required[], props } 。 */
function bodySchema(doc, op) {
  if (!op) return null;
  const param = (op.parameters || []).find((p) => p.in === 'body');
  if (!param) return null;
  const resolved = param.schema?.$ref ? deref(doc, param.schema.$ref) : param.schema;
  if (!resolved) return null;
  return {
    name: param.schema?.$ref?.split('/').pop() || 'inline',
    required: resolved.required || [],
    props: resolved.properties || {},
  };
}

function queryParams(op) {
  return ((op || {}).parameters || []).filter((p) => p.in === 'query').map((p) => p.name);
}

function pathParams(op) {
  return ((op || {}).parameters || []).filter((p) => p.in === 'path').map((p) => p.name);
}

const json = (v) => JSON.stringify(v);
const goStr = (v) => JSON.stringify(String(v ?? ''));
const indent = (text, pad) =>
  text
    .split('\n')
    .map((line) => (line.length ? pad + line : line))
    .join('\n');

// ---------- 3. 校验 quick-routes ----------

const { doc, source: swaggerSource } = await loadSwagger();
const ops = swaggerOps(doc);
console.log(`swagger: ${swaggerSource}`);
console.log(`paths: ${Object.keys(doc.paths || {}).length}, definitions: ${Object.keys(doc.definitions || {}).length}`);

if (pinMode) {
  // --pin：把当前 swagger 固化成仓库快照，之后 CI 不再需要网络。
  fs.mkdirSync(snapshotDir, { recursive: true });
  const pinFile = path.join(snapshotDir, `${swaggerTag}.json`);
  fs.writeFileSync(pinFile, JSON.stringify(doc));
  console.log(`📌 已固化快照：${path.relative(root, pinFile)}（${(fs.statSync(pinFile).size / 1024).toFixed(0)} KB）`);
}

/**
 * 上游路径解析：先精确匹配规范化路径；未命中时按模板匹配
 * （/dashboard/base/no/no → /dashboard/base/{ioOption}/{netOption}）。
 * 1Panel 把 ioOption/netOption 这类开关做成路径段并允许字面量 no。
 */
function resolveUpstream(upstreamPath) {
  const exact = normalizePath(upstreamPath);
  if (ops.has(exact)) return { key: exact, methodMap: ops.get(exact) };
  const segs = exact.split('/');
  for (const [key, methodMap] of ops) {
    const keySegs = key.split('/');
    if (keySegs.length !== segs.length) continue;
    let ok = true;
    for (let i = 0; i < segs.length; i++) {
      if (/^\{[^}]+\}$/.test(keySegs[i])) continue;
      if (keySegs[i] !== segs[i]) {
        ok = false;
        break;
      }
    }
    if (ok) return { key, methodMap };
  }
  return null;
}


const seenPrefix = new Set();
for (const route of spec.routes) {
  if (seenPrefix.has(route.prefix)) fail(`重复的 prefix: ${route.prefix}`);
  seenPrefix.add(route.prefix);

  const pathVarNames = [...route.prefix.matchAll(/\{([^}]+)\}/g)].map((m) => m[1]).filter((n) => n !== 'serverId');
  const referenced = new Set();
  const notePathVar = (token) => {
    // token 形如 path:id / path:name / path:appInstallId=installId
    if (!token) return;
    const spec2 = token.replace(/^path:/, '');
    const [from] = spec2.split('=');
    referenced.add(from);
  };

  if (route.kind === 'local' || route.kind === 'aggregate') {
    if (route.upstream) fail(`${route.prefix}: kind=${route.kind} 不应声明 upstream`);
    continue;
  }
  if (!route.upstream) {
    fail(`${route.prefix}: kind=${route.kind} 必须声明 upstream`);
    continue;
  }

  for (const [method, up] of Object.entries(route.upstream)) {
    if (!route.methods.includes(method)) {
      fail(`${route.prefix}: upstream 声明了 ${method}，但 methods 里没有`);
    }
    const upstreamMethod = (up.as || method).toUpperCase();
    const resolved = resolveUpstream(up.path);
    if (!resolved) {
      fail(`${route.prefix}: 上游路径在 swagger 中不存在 → ${up.path}`);
      continue;
    }
    const op = resolved.methodMap.get(upstreamMethod);
    if (!op) {
      fail(
        `${route.prefix}: 上游 ${up.path} 在 swagger 中没有 ${upstreamMethod} 方法（可用：${[...resolved.methodMap.keys()].join(',')}）`,
      );
      continue;
    }

    // 上游路径参数必须能由对外路径补齐（模板匹配后逐个核对）
    const upstreamPathVars = [...normalizePath(up.path).matchAll(/\{([^}]+)\}/g)].map((m) => m[1]);
    for (const n of upstreamPathVars) {
      if (!pathVarNames.includes(n)) {
        fail(`${route.prefix}: 上游 ${up.path} 需要路径参数 {${n}}，但对外 prefix 没有对应占位符`);
      }
      referenced.add(n);
    }

    // 查询参数必须真实存在
    for (const [qk, qv] of Object.entries(up.query || {})) {
      if (!queryParams(op).includes(qk)) {
        fail(`${route.prefix}: 上游 ${up.path} 没有查询参数 ${qk}（可用：${queryParams(op).join(',') || '无'}）`);
      }
      notePathVar(qv);
    }
    notePathVar(up.bodyFrom);

    // 「对外 GET / 上游 search POST」这类分页接口由面板补默认体，
    // 必须覆盖 swagger 声明的 required；由 bodyFrom 注入路径参数的接口不需要 defaults。
    const schema = bodySchema(doc, op);
    if (up.as && !up.bodyFrom) {
      if (!up.defaults) {
        fail(`${route.prefix}: 上游 ${up.path} 需要面板填充默认请求体（defaults），因为对外方法 ${method} 与上游 ${upstreamMethod} 不同`);
      } else if (schema) {
        const missing = schema.required.filter((f) => !(f in up.defaults));
        if (missing.length) {
          fail(
            `${route.prefix}: 上游 ${up.path} 的 required 字段未被默认体覆盖 → ${missing.join(', ')}（definitions/${schema.name}）`,
          );
        }
        const unknown = Object.keys(up.defaults).filter((f) => !(f in schema.props));
        if (unknown.length) {
          fail(`${route.prefix}: 默认体含 swagger 未声明字段 → ${unknown.join(', ')}（definitions/${schema.name}）`);
        }
      }
    } else if (up.defaults && schema) {
      const unknown = Object.keys(up.defaults).filter((f) => !(f in schema.props));
      if (unknown.length) {
        fail(`${route.prefix}: 默认体含 swagger 未声明字段 → ${unknown.join(', ')}（definitions/${schema.name}）`);
      }
    }
  }

  for (const v of pathVarNames) {
    if (!referenced.has(v)) {
      fail(`${route.prefix}: 路径参数 {${v}} 没有被 upstream 使用（会静默丢弃调用方的值）`);
    }
  }
}

if (errors.length) {
  console.error('\n校验失败，未写入任何文件：');
  for (const e of errors) console.error(`  - ${e}`);
  process.exit(1);
}

// ---------- 4. catalog.json ----------

const catalog = [];
for (const [p, methodMap] of ops) {
  for (const [method, op] of methodMap) {
    catalog.push({
      m: method.toLowerCase(),
      p,
      s: op.summary || '',
      ...(op.tags?.[0] ? { t: op.tags[0] } : {}),
    });
  }
}
catalog.sort((a, b) => (a.p === b.p ? a.m.localeCompare(b.m) : a.p.localeCompare(b.p)));

// ---------- 5. routes_gen.go ----------

const goRoutes = spec.routes
  .map((route) => {
    const upstreams = route.upstream
      ? Object.entries(route.upstream)
          .map(([method, up]) => {
            const fields = [
              `Method: ${goStr(method)}`,
              `Upstream: ${goStr(up.path)}`,
              up.as ? `As: ${goStr(up.as.toUpperCase())}` : null,
              up.defaults ? `Defaults: ${goStr(json(up.defaults))}` : null,
              up.bodyFrom ? `BodyFrom: ${goStr(up.bodyFrom)}` : null,
              up.query ? `Query: map[string]string{${Object.entries(up.query).map(([k, v]) => `${goStr(k)}: ${goStr(v)}`).join(', ')}}` : null,
            ].filter(Boolean);
            return `{${fields.join(', ')}}`;
          })
          .join(', ')
      : '';
    const queries = (route.query || []).map(goStr).join(', ');
    return `	{
		Prefix:      ${goStr(route.prefix)},
		Module:      ${goStr(route.module || 'onepanel')},
		MatchMode:   ${goStr(route.matchMode || 'pattern')},
		Kind:        ${goStr(route.kind)},
		Methods:     []string{${route.methods.map(goStr).join(', ')}},
		Description: ${goStr(route.description)},
		Query:       []string{${queries}},
		Upstreams:   []quickUpstream{${upstreams}},
	},`;
  })
  .join('\n');

const routesGo = `// Code generated by tools/onepanel/gen-onepanel-catalog.mjs — DO NOT EDIT.
// 源：tools/onepanel/quick-routes.json + 1Panel ${swaggerTag} 官方 swagger。

package onepanel

// SwaggerVersion 是当前快捷路由表对齐的 1Panel 版本；改版本必须重跑生成器。
const SwaggerVersion = ${goStr(swaggerTag)}

// quickUpstream 描述对外方法到 1Panel 上游的一次映射。
type quickUpstream struct {
	// Method 是对外 HTTP 方法（会出现在文档与 AI 目录里）。
	Method string
	// Upstream 是 1Panel 的 /api/v2 路径，允许含 {param} 占位符。
	Upstream string
	// As 非空时表示上游实际使用的方法（如对外 GET、上游 POST search）。
	As string
	// Defaults 是 JSON 默认请求体，调用方传了请求体时以调用方为准（按键合并）。
	Defaults string
	// BodyFrom 形如 "path:id" 或 "path:appInstallId=installId"，把路径参数注入请求体。
	BodyFrom string
	// Query 形如 {"container": "path:name"}，把路径参数/常量注入查询串。
	Query map[string]string
}

// quickRoute 是一条对外快捷路由。
type quickRoute struct {
	Prefix      string
	Module      string
	MatchMode   string
	Kind        string // local | aggregate | proxy
	Methods     []string
	Description string
	Query       []string
	Upstreams   []quickUpstream
}

// quickRoutes 是快捷控制接口的完整清单，service 分发与文档生成都以它为唯一事实源。
var quickRoutes = []quickRoute{
${goRoutes}
}

// quickRouteByPrefix 便于分发时按 prefix 精确查找（served 段用形状匹配，这里只做校验）。
func quickRouteByPrefix(prefix string) (quickRoute, bool) {
	for _, r := range quickRoutes {
		if r.Prefix == prefix {
			return r, true
		}
	}
	return quickRoute{}, false
}
`;

// ---------- 6. system/onepanel_gen.go ----------

const PROXY_PREFIX = '/api/onepanel';
const seeds = [];
const contracts = [];
const descriptions = [];

for (const route of spec.routes) {
  const isLocal = route.kind === 'local';
  const methodsGo = `[]string{${route.methods.map(goStr).join(', ')}}`;
  // 未显式声明时按形状推断：带 {param} 的是 pattern，否则是 exact。
  // 用 MatchPrefix 会让叶子路由被 AI 目录标成「聚合前缀，不可直接调用」。
  const inferredMode = route.prefix.includes('{') ? 'pattern' : 'exact';
  const mode = route.matchMode || inferredMode;
  const matchMode = mode === 'exact' ? 'manifest.MatchExact' : mode === 'pattern' ? 'manifest.MatchPattern' : 'manifest.MatchPrefix';
  const pathParams = [...route.prefix.matchAll(/\{([^}]+)\}/g)].map((m) => m[1]);
  const pathParamsGo = pathParams.length
    ? `PathParams: []apiDocParameter{${pathParams
        .map((n) => `{Name: ${goStr(n)}, In: "path", Required: true, Description: ${goStr(pathParamDescription(route.prefix, n))}}`)
        .join(', ')}},`
    : '';
  // 按 id 操作的接口统一补一条 note:Agent 最常踩的坑是「跨主机复用 id」或「用过期的 id」。
  const notesGo = pathParams.includes('id')
    ? `Notes: []string{${goStr('该接口按资源 id 操作:先调用对应列表接口取回 id 再按 id 调用;每台 1Panel 的 id 相互独立,不要跨主机复用。')}},`
    : '';

  const queryGo = (route.query || []).length
    ? `QueryParams: []apiDocParameter{${route.query
        .map((q) => {
          const example = q === 'type' ? 'mysql' : q === 'tail' ? '200' : q === 'since' ? '1h' : '';
          return `{Name: ${goStr(q)}, In: "query", Required: false, Description: ${goStr(queryDesc(q))}${example ? `, Example: ${goStr(example)}` : ''}}`;
        })
        .join(', ')}},`
    : '';

  seeds.push(`	{
		Route: manifest.Route{Prefix: ${goStr(route.prefix)}, Module: ${goStr(route.module || 'onepanel')}, Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: ${goStr(route.description)}, MatchMode: ${matchMode}},
		Docs: apiRouteDocs{
			Detail:  ${goStr(route.description)},
			Methods: ${methodsGo},${pathParamsGo ? `\n\t\t\t${pathParamsGo}` : ''}${queryGo ? `\n\t\t\t${queryGo}` : ''}${notesGo ? `\n\t\t\t${notesGo}` : ''}
		},
	},`);

  descriptions.push(`	${goStr(route.prefix)}: ${goStr(route.description)},`);

  // 请求契约：本地/聚合路由用 quick-routes 里手写的 contract；
  // 代理路由按 1Panel swagger 的 definitions 生成（含必填/枚举/类型）。
  if (route.contract) {
    if (route.contract.noBody) {
      contracts.push(`	routeRequestContracts[${goStr(route.prefix)}] = obj(nil, map[string]prop{})`);
    } else {
      const fields = Object.entries(route.contract.fields || {}).map(([name, f]) => {
        const enumTag = f.enum ? `, e: []string{${f.enum.map((v) => goStr(v)).join(', ')}}` : '';
        return `\t\t${goStr(name)}: {t: ${goStr(f.type)}, req: ${(route.contract.required || []).includes(name)}${enumTag}, d: ${goStr(f.description || name)}},`;
      });
      contracts.push(`	routeRequestContracts[${goStr(route.prefix)}] = obj([]string{${(route.contract.required || [])
        .map(goStr)
        .join(', ')}}, map[string]prop{
${fields.join('\n')}
	})`);
    }
  }

  if (!isLocal && route.upstream) {
    // 请求体优先取写方法的 upstream（POST/PUT/PATCH/DELETE）；
    // 只有读方法的接口（如 search 分页）不需要契约。
    const writeMethod = ['POST', 'PUT', 'PATCH', 'DELETE'].find((m) => route.upstream[m]);
    const up = writeMethod ? route.upstream[writeMethod] : null;
    if (up) {
      const resolved = resolveUpstream(up.path);
      const op = resolved?.methodMap.get((up.as || writeMethod).toUpperCase());
      const schema = bodySchema(doc, op);
      if (schema && Object.keys(schema.props).length > 0) {
        const fields = [];
        const pathVarNames = pathParams.filter((n) => n !== 'serverId');
        for (const [name, prop] of Object.entries(schema.props)) {
          const required = schema.required.includes(name);
          // 由路径参数注入的字段不需要调用方在 body 里再传
          const injected = injectedField(name, up, pathVarNames);
          const type = goTypeOf(prop);
          const enumTag = prop.enum ? `, e: []string{${prop.enum.map((v) => goStr(v)).join(', ')}}` : '';
          const cn = fieldDescCN(name);
          const base = cn ? `${cn}(${schema.name}.${name})` : `${schema.name}.${name}`;
          const desc = `${base}${injected ? '（可由路径参数自动注入）' : ''}`;
          fields.push(`\t\t${goStr(name)}: {t: ${goStr(type)}, req: ${required && !injected}${enumTag}, d: ${goStr(desc)}},`);
        }
        contracts.push(`	routeRequestContracts[${goStr(route.prefix)}] = obj([]string{${schema.required
          .filter((f) => !injectedField(f, up, pathVarNames))
          .map(goStr)
          .join(', ')}}, map[string]prop{
${fields.join('\n')}
	})`);
      } else {
        // 写方法没有请求体（或 1Panel 侧无需字段）：显式登记空契约，
        // 避免「写接口无契约」被审计判为遗漏。
        contracts.push(`	routeRequestContracts[${goStr(route.prefix)}] = obj(nil, map[string]prop{})`);
      }
    }
  }
}

function injectedField(name, up, pathVars) {
  // bodyFrom 支持「对外名=上游字段名」的映射（path:appInstallId=installId），
  // 所以既要看左边的对外名，也要看右边注入到 body 的上游字段名。
  for (const token of (up.bodyFrom || '').split(',')) {
    const t = token.trim().replace(/^path:/, '');
    if (!t) continue;
    const eq = t.indexOf('=');
    const from = eq >= 0 ? t.slice(0, eq) : t;
    const to = eq >= 0 ? t.slice(eq + 1) : t;
    if (name === from || name === to) return true;
  }
  return pathVars.includes(name);
}

/** 路径参数语义:告诉 Agent 该 id 属于哪个资源、从哪个列表接口取,避免跨主机/过期 id。 */
function pathParamDescription(prefix, name) {
  if (name === 'serverId') return 'API Monitor 服务器 ID(先由 /api/server/accounts 或 /api/onepanel/config 获取)';
  if (name === 'appInstallId') return '已安装应用 ID(取自 /apps/installed 列表)';
  if (name === 'websiteId') return '网站 ID(取自 /websites/search 的 items[].id)';
  if (name === 'name') return '容器名(取自 /containers 列表)';
  if (name === 'type') return '模板类型(1Panel 默认站点类型,如 html / php)';
  if (name === 'id') {
    if (prefix.includes('/ssl/')) return '证书 ID(取自 /ssl/search 的 items[].id;每台 1Panel 的 id 相互独立)';
    if (prefix.includes('/websites/')) return '网站 ID(取自 /websites/search 的 items[].id;每台 1Panel 的 id 相互独立)';
    if (prefix.includes('/databases/')) return '数据库 ID(取自 /databases 列表)';
    return '资源主键 ID(取自对应列表接口;每台 1Panel 的 id 相互独立)';
  }
  return '路径参数';
}

/** 请求体字段的中文别名:让 find_api / list_apis 能按字段语义(如“备注”)召回接口。 */
function fieldDescCN(name) {
  // 说明:此 map 必须定义在函数体内(而非模块级 const),否则在下方 routes 循环
  // 首次调用时会触发 TDZ(暂时性死区)。
  const map = {
    description: '备注/描述',
    remark: '备注',
    alias: '别名',
    name: '名称',
    primaryDomain: '主域名',
    otherDomains: '附加域名(每行一个)',
    domains: '域名列表',
    autoRenew: '是否自动续签',
    provider: '签发方式',
    keyType: '密钥类型',
    pushDir: '是否推送目录',
    image: '镜像',
    imageName: '镜像名',
    operation: '操作(start/stop/restart 等)',
    operate: '操作',
    names: '容器名列表',
    password: '密码',
    content: '内容',
    type: '类型',
  };
  return map[name] || '';
}

function queryDesc(name) {
  const map = {
    type: '资源类型：mysql / mariadb / postgresql / redis / mongodb',
    tail: '只取最后 N 行日志',
    since: '时间窗口，如 1h / 30m',
    name: '按名称过滤',
    detailName: '按备份对象名过滤',
    domain: '域名(精确域名或 *.example.com 泛域名)',
  };
  return map[name] || name;
}

/** swagger definitions 里的描述通常是空串，退化为字段名 + 类型说明。 */
function swaggerComment(schemaName, field) {
  return `${schemaName}.${field}`;
}

function goTypeOf(prop) {
  const t = prop.type;
  if (t === 'array') return 'array';
  if (t === 'object') return 'object';
  if (t === 'integer') return 'integer';
  if (t === 'number') return 'number';
  if (t === 'boolean') return 'boolean';
  return 'string';
}

const methodTable = spec.routes
  .map((route) => `	${goStr(route.prefix)}: []string{${route.methods.map(goStr).join(', ')}},`)
  .join('\n');

const systemGo = `// Code generated by tools/onepanel/gen-onepanel-catalog.mjs — DO NOT EDIT.
// 源：tools/onepanel/quick-routes.json + 1Panel ${swaggerTag} 官方 swagger。

package system

import "github.com/iwvw/api-monitor/backend-go/internal/manifest"

// onepanelRouteMethods 是 1Panel 快捷路由的真实方法集合。
// inferRouteMethods 的通用推断对 MatchPattern 一律兜底 GET，会把 POST/DELETE
// 接口标成只读，导致 AI 按错误方法调用（405）或漏掉写能力。
var onepanelRouteMethods = map[string][]string{
${methodTable}
}

// onepanelGeneratedSeeds 由生成器产出，覆盖 onepanel 全部快捷路由的文档条目。
var onepanelGeneratedSeeds = []apiDocSeed{
${seeds.join('\n')}
}

func init() {
	// 文档种子追加在人工种子之后：routeDocs 的合并是「后者覆盖前者」，
	// 生成器是 onepanel 的唯一事实源，必须后写。
	apiDocSeeds = append(apiDocSeeds, onepanelGeneratedSeeds...)

	for prefix, desc := range onepanelDescriptions {
		routeDescriptions[prefix] = desc
	}

${contracts.join('\n\n')}
}

// onepanelDescriptions 是 onepanel 路由的中文描述，与 quick-routes.json 同步。
var onepanelDescriptions = map[string]string{
${descriptions.join('\n')}
}
`;

// ---------- 7. 写出 ----------

if (checkOnly) {
  const stale = [];
  const compare = (file, content) => {
    const cur = fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : '';
    if (cur !== content) stale.push(path.relative(root, file));
  };
  compare(catalogOut, JSON.stringify(catalog, null, 2) + '\n');
  compare(routesGoOut, routesGo);
  compare(systemGoOut, systemGo);
  if (stale.length) {
    console.error(`生成产物已过期，请运行 npm run onepanel:generate：\n  ${stale.join('\n  ')}`);
    process.exit(1);
  }
  console.log(`onepanel 生成产物已是最新（${spec.routes.length} 条快捷路由，${catalog.length} 个官方端点）`);
  process.exit(0);
}

fs.writeFileSync(catalogOut, JSON.stringify(catalog, null, 2) + '\n');
fs.writeFileSync(routesGoOut, routesGo);
fs.writeFileSync(systemGoOut, systemGo);

console.log(`✅ 已生成（1Panel ${swaggerTag}）：`);
console.log(`   ${path.relative(root, catalogOut)}       ${catalog.length} 个官方端点`);
console.log(`   ${path.relative(root, routesGoOut)}  ${spec.routes.length} 条快捷路由`);
console.log(`   ${path.relative(root, systemGoOut)}     ${seeds.length} 条文档 seed / ${contracts.length} 条请求契约`);
